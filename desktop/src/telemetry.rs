//! Secret-safe protocol telemetry DTOs and presentation projections.
//!
//! Wire DTOs deliberately contain only aggregate counts and user-authored goal
//! text. Serde ignores unknown fields by default, which keeps additive protocol
//! changes compatible without retaining provider-private payloads. Convert a DTO
//! with its `project` method before presenting it: projections validate signed
//! wire values, use checked arithmetic, and bound every user-visible string.

use std::{error::Error, fmt};

use serde::{Deserialize, Serialize};

pub const MAX_OBJECTIVE_LABEL_CHARS: usize = 96;
pub const MAX_BLOCKED_REASON_LABEL_CHARS: usize = 160;
pub const MAX_TELEMETRY_LABEL_CHARS: usize = 320;

/// A validation failure in public telemetry received from the RPC stream.
///
/// Errors identify only a field, never provider data or user-authored text.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TelemetryError {
    Negative(&'static str),
    Overflow(&'static str),
    Invalid(&'static str),
}

impl fmt::Display for TelemetryError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Negative(field) => write!(formatter, "negative telemetry field: {field}"),
            Self::Overflow(field) => write!(formatter, "telemetry field overflow: {field}"),
            Self::Invalid(field) => write!(formatter, "invalid telemetry field: {field}"),
        }
    }
}

impl Error for TelemetryError {}

/// Dependency-light mirror of `protocol.Usage`.
///
/// Signed integers match the Go JSON shape and let projection reject negative
/// values explicitly. JSON integers outside `i64` are rejected by serde before
/// projection. Cost and provider metadata are intentionally absent.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct UsageDto {
    #[serde(default)]
    pub input: i64,
    #[serde(default)]
    pub output: i64,
    #[serde(default)]
    pub reasoning: i64,
    #[serde(default)]
    pub cache_read: i64,
    #[serde(default)]
    pub cache_read_known: bool,
    #[serde(default)]
    pub cache_write: i64,
    #[serde(default)]
    pub total_tokens: i64,
    #[serde(default)]
    pub requests: i64,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct UsageTelemetry {
    pub input_tokens: u64,
    pub output_tokens: u64,
    pub reasoning_tokens: u64,
    pub cache_read_tokens: u64,
    pub cache_read_known: bool,
    pub cache_write_tokens: u64,
    pub cache_tokens: u64,
    pub total_tokens: u64,
    pub requests: u64,
}

impl UsageDto {
    pub fn project(&self) -> Result<UsageTelemetry, TelemetryError> {
        let input = nonnegative(self.input, "usage.input")?;
        let output = nonnegative(self.output, "usage.output")?;
        let reasoning = nonnegative(self.reasoning, "usage.reasoning")?;
        let cache_read = nonnegative(self.cache_read, "usage.cache_read")?;
        let cache_write = nonnegative(self.cache_write, "usage.cache_write")?;
        let reported_total = nonnegative(self.total_tokens, "usage.total_tokens")?;
        let requests = nonnegative(self.requests, "usage.requests")?;

        let cache_tokens = checked_protocol_sum(cache_read, cache_write, "usage.cache")?;
        if cache_read > input || cache_write > input || cache_tokens > input {
            return Err(TelemetryError::Invalid("usage.cache"));
        }
        if reasoning > output && output != 0 {
            return Err(TelemetryError::Invalid("usage.reasoning"));
        }

        let minimum_total = checked_protocol_sum(input, output, "usage.total_tokens")?;
        let total = if reported_total == 0 {
            minimum_total
        } else {
            if reported_total < minimum_total {
                return Err(TelemetryError::Invalid("usage.total_tokens"));
            }
            reported_total
        };

        Ok(UsageTelemetry {
            input_tokens: input,
            output_tokens: output,
            reasoning_tokens: reasoning,
            cache_read_tokens: cache_read,
            cache_read_known: self.cache_read_known || cache_read > 0,
            cache_write_tokens: cache_write,
            cache_tokens,
            total_tokens: total,
            requests,
        })
    }
}

impl UsageTelemetry {
    /// A short label suitable for a status strip or accessibility description.
    pub fn compact_label(self) -> String {
        bounded_join(
            &[
                format!("in {}", compact_tokens(self.input_tokens)),
                format!("out {}", compact_tokens(self.output_tokens)),
                format!("cache {}", compact_tokens(self.cache_tokens)),
                format!("total {}", compact_tokens(self.total_tokens)),
            ],
            MAX_TELEMETRY_LABEL_CHARS,
        )
    }
}

/// Count-only, secret-safe subset of the `context` RPC response.
///
/// Categories are omitted because desktop chrome only needs aggregate pressure;
/// prompt text, tool arguments/results, continuity state, and arbitrary maps are
/// never retained by this type.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ContextTelemetryDto {
    #[serde(default)]
    pub latest_request: bool,
    #[serde(default)]
    pub estimated_input_tokens: i64,
    #[serde(default)]
    pub fixed_context_tokens: i64,
    #[serde(default)]
    pub fixed_context_budget_tokens: i64,
    #[serde(default)]
    pub fixed_context_over_budget: bool,
    #[serde(default)]
    pub context_window: i64,
    #[serde(default)]
    pub usage: Option<UsageDto>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ContextCalibration {
    ProviderReported,
    LocalEstimate,
    Unavailable,
}

impl ContextCalibration {
    pub const fn label(self) -> &'static str {
        match self {
            Self::ProviderReported => "provider-calibrated",
            Self::LocalEstimate => "estimated",
            Self::Unavailable => "unknown",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WarningBand {
    Unknown,
    Healthy,
    Notice,
    Warning,
    Critical,
}

impl WarningBand {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Unknown => "unknown",
            Self::Healthy => "healthy",
            Self::Notice => "notice",
            Self::Warning => "warning",
            Self::Critical => "critical",
        }
    }

    /// Matches Snow's context pressure thresholds without floating-point math.
    pub fn for_context(used: u64, window: Option<u64>) -> Self {
        let Some(window) = window.filter(|window| *window > 0) else {
            return Self::Unknown;
        };
        let used = u128::from(used);
        let window = u128::from(window);
        if used * 10 >= window * 9 {
            Self::Critical
        } else if used * 10 >= window * 7 {
            Self::Warning
        } else if used * 10 >= window * 5 {
            Self::Notice
        } else {
            Self::Healthy
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ContextTelemetry {
    pub used_tokens: u64,
    pub window_tokens: Option<u64>,
    pub calibration: ContextCalibration,
    pub warning_band: WarningBand,
    /// Percentage in tenths, capped at 999.9% to keep labels bounded.
    pub percent_tenths: Option<u16>,
    pub fixed_context_tokens: u64,
    pub fixed_context_budget_tokens: Option<u64>,
    pub fixed_context_over_budget: bool,
    pub latest_request: bool,
}

impl ContextTelemetryDto {
    pub fn project(&self) -> Result<ContextTelemetry, TelemetryError> {
        let estimated = nonnegative(
            self.estimated_input_tokens,
            "context.estimated_input_tokens",
        )?;
        let fixed = nonnegative(self.fixed_context_tokens, "context.fixed_context_tokens")?;
        let fixed_budget = optional_positive(
            self.fixed_context_budget_tokens,
            "context.fixed_context_budget_tokens",
        )?;
        let window = optional_positive(self.context_window, "context.context_window")?;

        let (used, calibration) = match self.usage.as_ref() {
            Some(usage) => {
                let usage = usage.project()?;
                if usage.input_tokens > 0 {
                    (usage.total_tokens, ContextCalibration::ProviderReported)
                } else if estimated > 0 {
                    (estimated, ContextCalibration::LocalEstimate)
                } else {
                    (0, ContextCalibration::Unavailable)
                }
            }
            None if estimated > 0 => (estimated, ContextCalibration::LocalEstimate),
            None => (0, ContextCalibration::Unavailable),
        };

        let warning_band = WarningBand::for_context(used, window);
        let percent_tenths = window.map(|window| bounded_percent_tenths(used, window));
        Ok(ContextTelemetry {
            used_tokens: used,
            window_tokens: window,
            calibration,
            warning_band,
            percent_tenths,
            fixed_context_tokens: fixed,
            fixed_context_budget_tokens: fixed_budget,
            fixed_context_over_budget: self.fixed_context_over_budget
                || fixed_budget.is_some_and(|budget| fixed > budget),
            latest_request: self.latest_request,
        })
    }
}

impl ContextTelemetry {
    pub fn compact_label(self) -> String {
        let used = match self.calibration {
            ContextCalibration::LocalEstimate => format!("~{}", compact_tokens(self.used_tokens)),
            _ => compact_tokens(self.used_tokens),
        };
        let window = self
            .window_tokens
            .map(compact_tokens)
            .unwrap_or_else(|| "?".to_owned());
        let mut parts = vec![format!("context {used}/{window}")];
        if let Some(tenths) = self.percent_tenths {
            parts.push(format!("{}.{:01}%", tenths / 10, tenths % 10));
        }
        parts.push(self.warning_band.label().to_owned());
        bounded_join(&parts, MAX_TELEMETRY_LABEL_CHARS)
    }
}

/// Pending interaction shown while a run remains active.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PendingStateDto {
    #[default]
    None,
    Permission,
    UserInput,
    Retry,
    Abort,
    #[serde(other)]
    Unknown,
}

impl PendingStateDto {
    pub const fn label(self) -> &'static str {
        match self {
            Self::None => "running",
            Self::Permission => "awaiting permission",
            Self::UserInput => "awaiting input",
            Self::Retry => "retrying",
            Self::Abort => "stopping",
            Self::Unknown => "pending",
        }
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct PendingInputsDto {
    #[serde(default)]
    pub steering: i64,
    #[serde(default)]
    pub follow_up: i64,
    /// Optional so an omitted additive aggregate can be derived safely.
    #[serde(default)]
    pub total: Option<i64>,
}

/// Local/run-event DTO. `elapsed_ms` is a duration, not a wall-clock timestamp.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct RunTelemetryDto {
    #[serde(default)]
    pub elapsed_ms: i64,
    #[serde(default)]
    pub active: bool,
    #[serde(default, alias = "pending_state")]
    pub pending: PendingStateDto,
    #[serde(default)]
    pub pending_inputs: PendingInputsDto,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RunTelemetry {
    pub elapsed_ms: u64,
    pub active: bool,
    pub pending: PendingStateDto,
    pub steering_pending: u64,
    pub follow_up_pending: u64,
    pub pending_inputs: u64,
}

impl RunTelemetryDto {
    pub fn project(&self) -> Result<RunTelemetry, TelemetryError> {
        let elapsed_ms = nonnegative(self.elapsed_ms, "run.elapsed_ms")?;
        let steering = nonnegative(self.pending_inputs.steering, "run.pending_inputs.steering")?;
        let follow_up = nonnegative(
            self.pending_inputs.follow_up,
            "run.pending_inputs.follow_up",
        )?;
        let derived_total = checked_protocol_sum(steering, follow_up, "run.pending_inputs.total")?;
        let total = match self.pending_inputs.total {
            None => derived_total,
            Some(value) => {
                let value = nonnegative(value, "run.pending_inputs.total")?;
                if value != derived_total {
                    return Err(TelemetryError::Invalid("run.pending_inputs.total"));
                }
                value
            }
        };
        if !self.active && self.pending != PendingStateDto::None {
            return Err(TelemetryError::Invalid("run.pending"));
        }

        Ok(RunTelemetry {
            elapsed_ms,
            active: self.active,
            pending: self.pending,
            steering_pending: steering,
            follow_up_pending: follow_up,
            pending_inputs: total,
        })
    }
}

impl RunTelemetry {
    pub fn compact_label(self) -> String {
        let state = if self.active {
            self.pending.label()
        } else {
            "idle"
        };
        let mut parts = vec![compact_duration_ms(self.elapsed_ms), state.to_owned()];
        if self.pending_inputs > 0 {
            parts.push(format!("{} queued", self.pending_inputs));
        }
        bounded_join(&parts, MAX_TELEMETRY_LABEL_CHARS)
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GoalStatus {
    Active,
    Paused,
    Blocked,
    UsageLimited,
    BudgetLimited,
    Complete,
    #[default]
    #[serde(other)]
    Unknown,
}

impl GoalStatus {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Active => "active",
            Self::Paused => "paused",
            Self::Blocked => "blocked",
            Self::UsageLimited => "usage limited",
            Self::BudgetLimited => "budget limited",
            Self::Complete => "complete",
            Self::Unknown => "unknown",
        }
    }
}

/// Public subset of a thread goal. IDs, costs, provider state, and timestamps
/// are deliberately not captured.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct GoalTelemetryDto {
    #[serde(default)]
    pub status: GoalStatus,
    #[serde(default)]
    pub objective: String,
    #[serde(default)]
    pub token_budget: Option<i64>,
    #[serde(default)]
    pub tokens_used: i64,
    #[serde(default)]
    pub seconds_used: i64,
    #[serde(default)]
    pub blocked_reason: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BudgetBand {
    Healthy,
    Warning,
    Critical,
    Exhausted,
}

impl BudgetBand {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Healthy => "within budget",
            Self::Warning => "budget warning",
            Self::Critical => "budget critical",
            Self::Exhausted => "budget exhausted",
        }
    }

    pub fn for_usage(used: u64, budget: u64) -> Self {
        let used = u128::from(used);
        let budget = u128::from(budget);
        if used >= budget {
            Self::Exhausted
        } else if used * 10 >= budget * 9 {
            Self::Critical
        } else if used * 10 >= budget * 7 {
            Self::Warning
        } else {
            Self::Healthy
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GoalTelemetry {
    pub status: GoalStatus,
    pub objective: String,
    pub token_budget: Option<u64>,
    pub tokens_used: u64,
    pub remaining_tokens: Option<u64>,
    pub budget_band: Option<BudgetBand>,
    pub seconds_used: u64,
    pub blocked_reason: Option<String>,
}

impl GoalTelemetryDto {
    pub fn project(&self) -> Result<GoalTelemetry, TelemetryError> {
        let tokens_used = nonnegative(self.tokens_used, "goal.tokens_used")?;
        let seconds_used = nonnegative(self.seconds_used, "goal.seconds_used")?;
        let token_budget = match self.token_budget {
            None => None,
            Some(value) if value <= 0 => {
                return Err(if value < 0 {
                    TelemetryError::Negative("goal.token_budget")
                } else {
                    TelemetryError::Invalid("goal.token_budget")
                });
            }
            Some(value) => Some(value as u64),
        };
        let objective = bounded_visible(&self.objective, MAX_OBJECTIVE_LABEL_CHARS);
        if objective.is_empty() {
            return Err(TelemetryError::Invalid("goal.objective"));
        }

        let blocked_reason = (self.status == GoalStatus::Blocked).then(|| {
            let reason = bounded_visible(&self.blocked_reason, MAX_BLOCKED_REASON_LABEL_CHARS);
            if reason.is_empty() {
                "No blocker reason recorded".to_owned()
            } else {
                reason
            }
        });
        let remaining_tokens = token_budget.map(|budget| budget.saturating_sub(tokens_used));
        let budget_band = token_budget.map(|budget| BudgetBand::for_usage(tokens_used, budget));

        Ok(GoalTelemetry {
            status: self.status,
            objective,
            token_budget,
            tokens_used,
            remaining_tokens,
            budget_band,
            seconds_used,
            blocked_reason,
        })
    }
}

impl GoalTelemetry {
    pub fn compact_label(&self) -> String {
        let budget = self
            .token_budget
            .map(|budget| {
                format!(
                    "{}/{} tokens",
                    compact_tokens(self.tokens_used),
                    compact_tokens(budget)
                )
            })
            .unwrap_or_else(|| format!("{} tokens", compact_tokens(self.tokens_used)));
        let mut parts = vec![
            self.status.label().to_owned(),
            self.objective.clone(),
            budget,
            compact_duration_seconds(self.seconds_used),
        ];
        if let Some(reason) = &self.blocked_reason {
            parts.push(format!("blocked: {reason}"));
        }
        bounded_join(&parts, MAX_TELEMETRY_LABEL_CHARS)
    }
}

fn nonnegative(value: i64, field: &'static str) -> Result<u64, TelemetryError> {
    u64::try_from(value).map_err(|_| TelemetryError::Negative(field))
}

fn optional_positive(value: i64, field: &'static str) -> Result<Option<u64>, TelemetryError> {
    match value {
        value if value < 0 => Err(TelemetryError::Negative(field)),
        0 => Ok(None),
        value => Ok(Some(value as u64)),
    }
}

fn checked_protocol_sum(left: u64, right: u64, field: &'static str) -> Result<u64, TelemetryError> {
    let total = left
        .checked_add(right)
        .ok_or(TelemetryError::Overflow(field))?;
    if total > i64::MAX as u64 {
        return Err(TelemetryError::Overflow(field));
    }
    Ok(total)
}

fn bounded_percent_tenths(used: u64, total: u64) -> u16 {
    debug_assert!(total > 0);
    let tenths = (u128::from(used) * 1_000 + u128::from(total) / 2) / u128::from(total);
    tenths.min(9_999) as u16
}

pub fn compact_tokens(value: u64) -> String {
    const UNITS: [&str; 7] = ["", "k", "m", "b", "t", "q", "e"];
    let mut divisor = 1_u64;
    let mut unit = 0;
    while unit + 1 < UNITS.len() && value >= divisor.saturating_mul(1_000) {
        divisor = divisor.saturating_mul(1_000);
        unit += 1;
    }
    if unit == 0 {
        return value.to_string();
    }

    let mut tenths = (u128::from(value) * 10 + u128::from(divisor) / 2) / u128::from(divisor);
    if tenths >= 10_000 && unit + 1 < UNITS.len() {
        unit += 1;
        tenths = (tenths + 500) / 1_000;
    }
    if tenths % 10 == 0 {
        format!("{}{}", tenths / 10, UNITS[unit])
    } else {
        format!("{}.{:01}{}", tenths / 10, tenths % 10, UNITS[unit])
    }
}

pub fn compact_duration_ms(elapsed_ms: u64) -> String {
    compact_duration_seconds(elapsed_ms / 1_000)
}

pub fn compact_duration_seconds(seconds: u64) -> String {
    match seconds {
        0..=59 => format!("{seconds}s"),
        60..=3_599 => format!("{}m {:02}s", seconds / 60, seconds % 60),
        3_600..=86_399 => format!("{}h {:02}m", seconds / 3_600, (seconds / 60) % 60),
        _ => format!("{}d {:02}h", seconds / 86_400, (seconds / 3_600) % 24),
    }
}

/// Collapse controls/whitespace and truncate by Unicode scalar count.
/// The returned string never exceeds `max_chars`, including the ellipsis.
pub fn bounded_visible(value: &str, max_chars: usize) -> String {
    if max_chars == 0 {
        return String::new();
    }

    let mut output = String::with_capacity(max_chars.min(value.len()));
    let mut pending_space = false;
    let mut truncated = false;
    for character in value.chars() {
        if character.is_control() || character.is_whitespace() {
            pending_space = !output.is_empty();
            continue;
        }
        let extra = usize::from(pending_space);
        if output.chars().count() + extra + 1 > max_chars {
            truncated = true;
            break;
        }
        if pending_space {
            output.push(' ');
            pending_space = false;
        }
        output.push(character);
    }

    if truncated {
        let keep = max_chars.saturating_sub(1);
        output = output.chars().take(keep).collect();
        while output.ends_with(' ') {
            output.pop();
        }
        output.push('…');
    }
    output
}

fn bounded_join(parts: &[String], max_chars: usize) -> String {
    bounded_visible(&parts.join(" · "), max_chars)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn context_warning_bands_match_surface_thresholds() {
        assert_eq!(
            WarningBand::for_context(499, Some(1_000)),
            WarningBand::Healthy
        );
        assert_eq!(
            WarningBand::for_context(500, Some(1_000)),
            WarningBand::Notice
        );
        assert_eq!(
            WarningBand::for_context(699, Some(1_000)),
            WarningBand::Notice
        );
        assert_eq!(
            WarningBand::for_context(700, Some(1_000)),
            WarningBand::Warning
        );
        assert_eq!(
            WarningBand::for_context(899, Some(1_000)),
            WarningBand::Warning
        );
        assert_eq!(
            WarningBand::for_context(900, Some(1_000)),
            WarningBand::Critical
        );
        assert_eq!(
            WarningBand::for_context(u64::MAX, Some(1)),
            WarningBand::Critical
        );
        assert_eq!(WarningBand::for_context(1, None), WarningBand::Unknown);
    }

    #[test]
    fn context_projection_prefers_valid_provider_total_and_marks_calibration() {
        let context = ContextTelemetryDto {
            latest_request: true,
            estimated_input_tokens: 800,
            context_window: 2_000,
            usage: Some(UsageDto {
                input: 1_000,
                output: 100,
                total_tokens: 1_100,
                ..UsageDto::default()
            }),
            ..ContextTelemetryDto::default()
        }
        .project()
        .unwrap();

        assert_eq!(context.used_tokens, 1_100);
        assert_eq!(context.calibration, ContextCalibration::ProviderReported);
        assert_eq!(context.warning_band, WarningBand::Notice);
        assert_eq!(context.compact_label(), "context 1.1k/2k · 55.0% · notice");
    }

    #[test]
    fn goal_budgets_saturate_remaining_and_have_bands() {
        let cases = [
            (699, BudgetBand::Healthy, 301),
            (700, BudgetBand::Warning, 300),
            (900, BudgetBand::Critical, 100),
            (1_000, BudgetBand::Exhausted, 0),
            (1_200, BudgetBand::Exhausted, 0),
        ];
        for (used, expected_band, expected_remaining) in cases {
            let goal = GoalTelemetryDto {
                status: GoalStatus::Active,
                objective: "ship desktop telemetry".into(),
                token_budget: Some(1_000),
                tokens_used: used,
                ..GoalTelemetryDto::default()
            }
            .project()
            .unwrap();
            assert_eq!(goal.budget_band, Some(expected_band));
            assert_eq!(goal.remaining_tokens, Some(expected_remaining));
        }
    }

    #[test]
    fn blocked_goal_has_safe_reason_and_migration_fallback() {
        let goal = GoalTelemetryDto {
            status: GoalStatus::Blocked,
            objective: "release".into(),
            blocked_reason: " CI\n\u{1b}[31m unavailable ".into(),
            seconds_used: 62,
            ..GoalTelemetryDto::default()
        }
        .project()
        .unwrap();
        assert_eq!(goal.blocked_reason.as_deref(), Some("CI [31m unavailable"));
        assert!(
            goal.compact_label()
                .contains("blocked: CI [31m unavailable")
        );
        assert!(goal.compact_label().contains("1m 02s"));

        let migrated = GoalTelemetryDto {
            status: GoalStatus::Blocked,
            objective: "release".into(),
            ..GoalTelemetryDto::default()
        }
        .project()
        .unwrap();
        assert_eq!(
            migrated.blocked_reason.as_deref(),
            Some("No blocker reason recorded")
        );
    }

    #[test]
    fn objective_reason_and_final_label_are_unicode_bounded() {
        let goal = GoalTelemetryDto {
            status: GoalStatus::Blocked,
            objective: "🦀".repeat(MAX_OBJECTIVE_LABEL_CHARS + 20),
            blocked_reason: "é".repeat(MAX_BLOCKED_REASON_LABEL_CHARS + 20),
            token_budget: Some(20_000),
            tokens_used: 1_200,
            seconds_used: 4_000,
        }
        .project()
        .unwrap();

        assert_eq!(goal.objective.chars().count(), MAX_OBJECTIVE_LABEL_CHARS);
        assert!(goal.objective.ends_with('…'));
        assert_eq!(
            goal.blocked_reason.as_ref().unwrap().chars().count(),
            MAX_BLOCKED_REASON_LABEL_CHARS
        );
        let label = goal.compact_label();
        assert!(label.chars().count() <= MAX_TELEMETRY_LABEL_CHARS);
        assert!(label.ends_with('…'));
    }

    #[test]
    fn run_projects_elapsed_and_pending_counts() {
        let run = RunTelemetryDto {
            elapsed_ms: 62_900,
            active: true,
            pending: PendingStateDto::UserInput,
            pending_inputs: PendingInputsDto {
                steering: 1,
                follow_up: 2,
                total: Some(3),
            },
        }
        .project()
        .unwrap();
        assert_eq!(run.compact_label(), "1m 02s · awaiting input · 3 queued");
    }

    #[test]
    fn omitted_additive_fields_default_and_unknown_fields_are_not_retained() {
        let usage: UsageDto = serde_json::from_str(r#"{"input":4,"future":99}"#).unwrap();
        assert_eq!(usage.project().unwrap().total_tokens, 4);

        let goal: GoalTelemetryDto = serde_json::from_str(
            r#"{"status":"future_state","objective":"safe","provider_private":{"opaque":"secret"}}"#,
        )
        .unwrap();
        assert_eq!(goal.status, GoalStatus::Unknown);
        let encoded = serde_json::to_string(&goal).unwrap();
        assert!(!encoded.contains("provider_private"));
        assert!(!encoded.contains("secret"));
    }

    #[test]
    fn negatives_inconsistency_and_json_overflow_fail_safely() {
        assert_eq!(
            UsageDto {
                input: -1,
                ..UsageDto::default()
            }
            .project(),
            Err(TelemetryError::Negative("usage.input"))
        );
        assert_eq!(
            UsageDto {
                input: i64::MAX,
                output: i64::MAX,
                ..UsageDto::default()
            }
            .project(),
            Err(TelemetryError::Overflow("usage.total_tokens"))
        );
        assert_eq!(
            RunTelemetryDto {
                active: true,
                pending_inputs: PendingInputsDto {
                    steering: 1,
                    follow_up: 1,
                    total: Some(3),
                },
                ..RunTelemetryDto::default()
            }
            .project(),
            Err(TelemetryError::Invalid("run.pending_inputs.total"))
        );
        assert!(serde_json::from_str::<UsageDto>(r#"{"input":9223372036854775808}"#).is_err());
    }
}
