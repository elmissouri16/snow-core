//! Pure provider-catalog and model-presentation helpers for the desktop UI.
//!
//! The RPC server owns provider ordering and metadata. These helpers deliberately
//! preserve that ordering, retain unknown capability states, and never infer
//! provider pricing or model capabilities from names.

use crate::snow::{AuthProvider, ModelInfo};

const MAX_PROVIDER_ID_CHARS: usize = 256;
const MAX_PROVIDER_LABEL_CHARS: usize = 256;
const MAX_STATUS_CHARS: usize = 512;
const MAX_METHODS: usize = 32;
const MAX_METHOD_CHARS: usize = 128;
const MAX_SEARCH_QUERY_CHARS: usize = 256;
const MAX_SEARCH_RESULTS: usize = 256;
const MAX_MODEL_FIELD_CHARS: usize = 2_048;
const MAX_MODEL_EFFORTS: usize = 64;

/// Whether a provider belongs in the interactive desktop catalog.
///
/// `fake` is Snow's deterministic internal runtime adapter. It remains valid
/// for tests and local process startup, but is not a user-selectable provider.
pub fn is_user_visible_provider(provider_id: &str) -> bool {
    provider_id.trim() != "fake"
}

/// A bounded, presentation-safe provider row built from the server inventory.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProviderCatalogItem {
    pub id: String,
    pub label: String,
    pub required: bool,
    pub active: bool,
    pub synthesized: bool,
    pub status: ProviderStatus,
    /// Bounded, display-ready method names in server order.
    pub methods: Vec<String>,
    search_text: String,
}

/// Authentication availability shown independently of provider identity.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProviderStatus {
    /// The provider can be used without a credential. `optional_state` retains
    /// whether an optional credential is configured or missing.
    NoAuthenticationRequired {
        optional_state: Option<String>,
        summary: String,
    },
    Configured {
        summary: String,
    },
    NotConfigured {
        summary: String,
    },
    Expired {
        summary: String,
    },
    Invalid {
        summary: String,
    },
    Unknown {
        state: String,
        summary: String,
    },
}

impl ProviderStatus {
    pub fn label(&self) -> String {
        match self {
            Self::NoAuthenticationRequired {
                optional_state,
                summary,
            } => {
                let mut label = "No authentication required".to_owned();
                if let Some(state) = optional_state.as_deref() {
                    match state {
                        "configured" => label.push_str(" · Optional credential configured"),
                        "missing" => label.push_str(" · Optional credential not configured"),
                        "expired" => label.push_str(" · Optional credential expired"),
                        "invalid" => label.push_str(" · Optional credential invalid"),
                        state => {
                            label.push_str(" · Optional credential ");
                            label.push_str(state);
                        }
                    }
                }
                if !summary.is_empty() && !label.to_lowercase().contains(&summary.to_lowercase()) {
                    label.push_str(" · ");
                    label.push_str(summary);
                }
                label
            }
            Self::Configured { summary } => status_label("Configured", summary),
            Self::NotConfigured { summary } => status_label("Not configured", summary),
            Self::Expired { summary } => status_label("Credential expired", summary),
            Self::Invalid { summary } => status_label("Credential invalid", summary),
            Self::Unknown { state, summary } => {
                let prefix = if state.is_empty() {
                    "Status unknown"
                } else {
                    state
                };
                status_label(prefix, summary)
            }
        }
    }
}

fn status_label(prefix: &str, summary: &str) -> String {
    if summary.is_empty() || summary.eq_ignore_ascii_case(prefix) {
        prefix.to_owned()
    } else {
        format!("{prefix} · {summary}")
    }
}

/// Build the provider rows in server order.
///
/// Whitespace-only and pathologically long IDs are rejected. IDs are
/// normalized only by trimming; the first occurrence wins. The internal
/// `fake` adapter is omitted at this presentation boundary. If any other
/// effective runtime provider is absent, a final synthetic row keeps it
/// selectable and visible without inventing authentication metadata.
pub fn build_provider_catalog(
    providers: &[AuthProvider],
    active_provider: Option<&str>,
) -> Vec<ProviderCatalogItem> {
    let active = bounded_trimmed(active_provider.unwrap_or_default(), MAX_PROVIDER_ID_CHARS);
    let mut seen =
        std::collections::HashSet::with_capacity(providers.len().min(MAX_SEARCH_RESULTS));
    let mut rows = Vec::with_capacity(providers.len().saturating_add(1).min(MAX_SEARCH_RESULTS));

    for provider in providers.iter().take(MAX_SEARCH_RESULTS) {
        let Some(id) = bounded_trimmed(&provider.provider_id, MAX_PROVIDER_ID_CHARS) else {
            continue;
        };
        if !is_user_visible_provider(&id) || !seen.insert(id.clone()) {
            continue;
        }
        rows.push(provider_row(provider, id, active.as_deref()));
    }

    if let Some(active) = active
        && is_user_visible_provider(&active)
        && !seen.contains(&active)
        && rows.len() < MAX_SEARCH_RESULTS
    {
        let status = ProviderStatus::Unknown {
            state: String::new(),
            summary: "Not present in the current provider inventory".to_owned(),
        };
        rows.push(finalize_provider_row(
            active.clone(),
            active,
            false,
            true,
            true,
            status,
            Vec::new(),
            Vec::new(),
        ));
    }

    rows
}

fn provider_row(
    provider: &AuthProvider,
    id: String,
    active_provider: Option<&str>,
) -> ProviderCatalogItem {
    let label = bounded_trimmed(&provider.display_name, MAX_PROVIDER_LABEL_CHARS)
        .unwrap_or_else(|| id.clone());
    let state = bounded_trimmed(&provider.status.state, MAX_METHOD_CHARS).unwrap_or_default();
    let summary = bounded_trimmed(&provider.status.summary, MAX_STATUS_CHARS).unwrap_or_default();
    let status = if !provider.required {
        ProviderStatus::NoAuthenticationRequired {
            optional_state: (!state.is_empty()).then_some(state),
            summary,
        }
    } else {
        match state.as_str() {
            "configured" => ProviderStatus::Configured { summary },
            "missing" | "" => ProviderStatus::NotConfigured { summary },
            "expired" => ProviderStatus::Expired { summary },
            "invalid" => ProviderStatus::Invalid { summary },
            _ => ProviderStatus::Unknown { state, summary },
        }
    };
    let bounded_methods = provider
        .methods
        .iter()
        .take(MAX_METHODS)
        .collect::<Vec<_>>();
    let methods = bounded_methods
        .iter()
        .filter_map(|method| {
            bounded_trimmed(&method.display_name, MAX_METHOD_CHARS)
                .or_else(|| bounded_trimmed(&method.id, MAX_METHOD_CHARS))
        })
        .collect();
    let method_search_terms = bounded_methods
        .iter()
        .flat_map(|method| [&method.id, &method.display_name, &method.kind])
        .filter_map(|field| bounded_trimmed(field, MAX_METHOD_CHARS))
        .collect();
    let active = active_provider.is_some_and(|active| active == id);
    finalize_provider_row(
        id,
        label,
        provider.required,
        active,
        false,
        status,
        methods,
        method_search_terms,
    )
}

fn finalize_provider_row(
    id: String,
    label: String,
    required: bool,
    active: bool,
    synthesized: bool,
    status: ProviderStatus,
    methods: Vec<String>,
    search_aliases: Vec<String>,
) -> ProviderCatalogItem {
    let mut search_parts = vec![id.clone(), label.clone(), status.label()];
    search_parts.extend(methods.iter().cloned());
    search_parts.extend(search_aliases);
    let search_text = search_parts.join("\n").to_lowercase();
    ProviderCatalogItem {
        id,
        label,
        required,
        active,
        synthesized,
        status,
        methods,
        search_text,
    }
}

/// Search provider ID, display name, status, and authentication methods.
///
/// The query and result count are bounded, and catalog fields were bounded at
/// construction time, so adversarial server metadata cannot force unbounded
/// scanning or allocation in the picker.
pub fn search_provider_catalog(items: &[ProviderCatalogItem], query: &str) -> Vec<usize> {
    let query = query
        .trim()
        .chars()
        .take(MAX_SEARCH_QUERY_CHARS)
        .collect::<String>()
        .to_lowercase();
    items
        .iter()
        .enumerate()
        .filter(|(_, item)| query.is_empty() || item.search_text.contains(&query))
        .map(|(index, _)| index)
        .take(MAX_SEARCH_RESULTS)
        .collect()
}

/// Resolve a provider ID to its catalog label without hiding unknown IDs.
/// The internal fake adapter receives a neutral chooser label.
pub fn provider_label(items: &[ProviderCatalogItem], provider_id: &str) -> String {
    let normalized = provider_id.trim();
    if !is_user_visible_provider(normalized) {
        return "Choose provider".to_owned();
    }
    items
        .iter()
        .find(|item| item.id == normalized)
        .map(|item| item.label.clone())
        .unwrap_or_else(|| normalized.to_owned())
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReasoningSummarySupport {
    Supported,
    Unsupported,
    Unknown,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ModelPresentation {
    pub provider: String,
    pub id: String,
    pub label: String,
    /// Provider-owned privacy, retention, or training notice. Kept prominent
    /// and separate from generic capability text.
    pub privacy_description: String,
    pub effective_context_tokens: Option<u64>,
    pub maximum_context_tokens: Option<u64>,
    pub maximum_output_tokens: Option<u64>,
    pub supports_tools: bool,
    pub supports_thinking: bool,
    pub supports_vision: bool,
    pub supports_verbosity: bool,
    pub reasoning_summary: ReasoningSummarySupport,
    /// Exact provider order and values; no name-based guesses or normalization.
    pub thinking_efforts: Vec<String>,
    /// Exact provider default; absent when the catalog supplied an empty value.
    pub default_thinking_effort: Option<String>,
    pub pricing: Option<ModelPricingPresentation>,
    pub upgrade: Option<ModelUpgradePresentation>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ModelPricingPresentation {
    /// Empty catalog currency remains `None`; the desktop must never assume USD.
    pub currency: Option<String>,
    pub input_per_million: f64,
    pub output_per_million: f64,
    pub cache_read_per_million: f64,
    pub cache_write_per_million: f64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ModelUpgradePresentation {
    pub target_model: String,
    pub message: String,
}

/// Project one model into explicit UI metadata without inferring missing facts.
pub fn model_presentation(model: &ModelInfo) -> ModelPresentation {
    let id = bounded(&model.id, MAX_MODEL_FIELD_CHARS);
    let label =
        bounded_trimmed(&model.display_name, MAX_MODEL_FIELD_CHARS).unwrap_or_else(|| id.clone());
    let reasoning_summary = match model.supports_reasoning_summary {
        Some(true) => ReasoningSummarySupport::Supported,
        Some(false) => ReasoningSummarySupport::Unsupported,
        None => ReasoningSummarySupport::Unknown,
    };
    let thinking_efforts = model
        .thinking_levels
        .iter()
        .take(MAX_MODEL_EFFORTS)
        .map(|effort| bounded(effort, MAX_METHOD_CHARS))
        .collect();
    let default_thinking_effort = (!model.default_thinking.is_empty())
        .then(|| bounded(&model.default_thinking, MAX_METHOD_CHARS));
    let pricing = model
        .pricing
        .as_ref()
        .map(|pricing| ModelPricingPresentation {
            currency: bounded_trimmed(&pricing.currency, MAX_METHOD_CHARS),
            input_per_million: pricing.input_per_million,
            output_per_million: pricing.output_per_million,
            cache_read_per_million: pricing.cache_read_per_million,
            cache_write_per_million: pricing.cache_write_per_million,
        });
    let upgrade = model
        .upgrade
        .as_ref()
        .map(|upgrade| ModelUpgradePresentation {
            target_model: bounded(&upgrade.model, MAX_MODEL_FIELD_CHARS),
            message: bounded(&upgrade.message, MAX_MODEL_FIELD_CHARS),
        });

    ModelPresentation {
        provider: bounded(&model.provider, MAX_MODEL_FIELD_CHARS),
        id,
        label,
        privacy_description: bounded(&model.description, MAX_MODEL_FIELD_CHARS),
        effective_context_tokens: (model.context_window > 0).then_some(model.context_window),
        maximum_context_tokens: (model.max_context_window > 0).then_some(model.max_context_window),
        maximum_output_tokens: (model.max_output_tokens > 0).then_some(model.max_output_tokens),
        supports_tools: model.supports_tools,
        supports_thinking: model.supports_thinking,
        supports_vision: model.supports_vision,
        supports_verbosity: model.supports_verbosity,
        reasoning_summary,
        thinking_efforts,
        default_thinking_effort,
        pricing,
        upgrade,
    }
}

/// Model picker search includes provider identity, the provider-owned privacy
/// notice, capabilities, exact efforts/default, pricing currency, and upgrade
/// metadata. Search remains bounded in both work and results.
pub fn search_models(models: &[ModelInfo], query: &str) -> Vec<usize> {
    let query = query
        .trim()
        .chars()
        .take(MAX_SEARCH_QUERY_CHARS)
        .collect::<String>()
        .to_lowercase();
    models
        .iter()
        .take(MAX_SEARCH_RESULTS)
        .enumerate()
        .filter_map(|(index, model)| {
            let projection = model_presentation(model);
            let mut fields = vec![
                projection.provider,
                projection.id,
                projection.label,
                projection.privacy_description,
                format!("reasoning summary {:?}", projection.reasoning_summary),
                format!(
                    "tools {} thinking {} vision {} verbosity {}",
                    projection.supports_tools,
                    projection.supports_thinking,
                    projection.supports_vision,
                    projection.supports_verbosity
                ),
            ];
            fields.extend(projection.thinking_efforts);
            if let Some(default) = projection.default_thinking_effort {
                fields.push(default);
            }
            if let Some(pricing) = projection.pricing
                && let Some(currency) = pricing.currency
            {
                fields.push(currency);
            }
            if let Some(upgrade) = projection.upgrade {
                fields.push(upgrade.target_model);
                fields.push(upgrade.message);
            }
            let matches = query.is_empty()
                || fields
                    .iter()
                    .any(|field| field.to_lowercase().contains(&query));
            matches.then_some(index)
        })
        .take(MAX_SEARCH_RESULTS)
        .collect()
}

pub fn model_label(model: &ModelInfo) -> String {
    model_presentation(model).label
}

fn bounded_trimmed(value: &str, max_chars: usize) -> Option<String> {
    let trimmed = value.trim();
    if trimmed.is_empty() || trimmed.chars().count() > max_chars {
        return None;
    }
    Some(trimmed.to_owned())
}

fn bounded(value: &str, max_chars: usize) -> String {
    if value.chars().count() <= max_chars {
        value.to_owned()
    } else {
        value.chars().take(max_chars).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::snow::{AuthMethod, AuthStatus, ModelPricing, ModelUpgrade};

    fn provider(id: &str, label: &str, required: bool, state: &str) -> AuthProvider {
        AuthProvider {
            provider_id: id.into(),
            display_name: label.into(),
            required,
            methods: vec![AuthMethod {
                id: "device".into(),
                display_name: "Device code".into(),
                kind: "oauth".into(),
            }],
            status: AuthStatus {
                provider_id: id.trim().into(),
                state: state.into(),
                summary: if state == "missing" {
                    "not configured".into()
                } else {
                    String::new()
                },
                ..AuthStatus::default()
            },
            ..AuthProvider::default()
        }
    }

    #[test]
    fn provider_catalog_preserves_order_dedupes_and_synthesizes_active() {
        let providers = vec![
            provider(" zen ", "Zen", false, "missing"),
            provider("", "Empty", true, "missing"),
            provider("go", "", true, "configured"),
            provider("zen", "Duplicate", true, "configured"),
        ];
        let rows = build_provider_catalog(&providers, Some(" custom "));
        assert_eq!(
            rows.iter().map(|row| row.id.as_str()).collect::<Vec<_>>(),
            ["zen", "go", "custom"]
        );
        assert_eq!(rows[1].label, "go");
        assert!(rows[2].active);
        assert!(rows[2].synthesized);
        assert_eq!(provider_label(&rows, "custom"), "custom");
        assert_eq!(provider_label(&rows, "missing"), "missing");
    }

    #[test]
    fn fake_provider_is_filtered_whether_explicit_or_synthesized() {
        let providers = vec![
            provider("fake", "Synthetic test provider", false, "configured"),
            provider("go", "OpenCode Go", true, "configured"),
        ];
        let explicit = build_provider_catalog(&providers, Some("fake"));
        assert_eq!(
            explicit
                .iter()
                .map(|row| row.id.as_str())
                .collect::<Vec<_>>(),
            ["go"]
        );
        assert!(!explicit.iter().any(|row| row.active || row.synthesized));

        let synthesized = build_provider_catalog(&providers[1..], Some(" fake "));
        assert_eq!(
            synthesized
                .iter()
                .map(|row| row.id.as_str())
                .collect::<Vec<_>>(),
            ["go"]
        );
        assert_eq!(provider_label(&synthesized, "fake"), "Choose provider");
        assert!(!is_user_visible_provider(" fake "));
    }

    #[test]
    fn unknown_non_test_provider_remains_visible_and_uses_raw_fallback() {
        let rows = build_provider_catalog(&[], Some(" private-compatible "));
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].id, "private-compatible");
        assert!(rows[0].active);
        assert!(rows[0].synthesized);
        assert_eq!(
            provider_label(&rows, "private-compatible"),
            "private-compatible"
        );
        assert!(is_user_visible_provider("private-compatible"));
    }

    #[test]
    fn active_provider_is_not_duplicated() {
        let rows = build_provider_catalog(&[provider("go", "Go", true, "configured")], Some("go"));
        assert_eq!(rows.len(), 1);
        assert!(rows[0].active);
        assert!(!rows[0].synthesized);
    }

    #[test]
    fn statuses_distinguish_no_auth_and_not_configured() {
        let rows = build_provider_catalog(
            &[
                provider("free", "Free", false, "missing"),
                provider("paid", "Paid", true, "missing"),
            ],
            None,
        );
        assert!(matches!(
            rows[0].status,
            ProviderStatus::NoAuthenticationRequired {
                optional_state: Some(ref state),
                ..
            } if state == "missing"
        ));
        assert!(
            rows[0]
                .status
                .label()
                .contains("No authentication required")
        );
        assert!(matches!(
            rows[1].status,
            ProviderStatus::NotConfigured { .. }
        ));
        assert!(rows[1].status.label().contains("Not configured"));
    }

    #[test]
    fn provider_search_covers_status_and_methods_and_is_bounded() {
        let rows = build_provider_catalog(
            &[
                provider("free", "Community", false, "missing"),
                provider("go", "OpenCode Go", true, "configured"),
            ],
            None,
        );
        assert_eq!(search_provider_catalog(&rows, "optional credential"), [0]);
        assert_eq!(search_provider_catalog(&rows, "device code"), [0, 1]);
        assert_eq!(search_provider_catalog(&rows, "oauth"), [0, 1]);
        assert_eq!(search_provider_catalog(&rows, "device"), [0, 1]);
        assert_eq!(search_provider_catalog(&rows, "not configured"), [0]);
        assert_eq!(search_provider_catalog(&rows, "opencode"), [1]);
        assert_eq!(search_provider_catalog(&rows, ""), [0, 1]);
    }

    #[test]
    fn model_projection_preserves_explicit_catalog_facts() {
        let model = ModelInfo {
            provider: "provider".into(),
            id: "model-v2".into(),
            display_name: "Model V2".into(),
            description: "Training and retention notice".into(),
            context_window: 120_000,
            max_context_window: 200_000,
            max_output_tokens: 16_000,
            supports_tools: true,
            supports_thinking: true,
            supports_vision: true,
            supports_verbosity: false,
            supports_reasoning_summary: None,
            default_thinking: "balanced-exact".into(),
            thinking_levels: vec!["small-exact".into(), "balanced-exact".into()],
            pricing: Some(ModelPricing {
                currency: String::new(),
                input_per_million: 1.25,
                output_per_million: 2.5,
                cache_read_per_million: 0.125,
                cache_write_per_million: 1.5,
            }),
            upgrade: Some(ModelUpgrade {
                model: "model-v3".into(),
                message: "Upgrade for a larger verified window".into(),
            }),
        };
        let presentation = model_presentation(&model);
        assert_eq!(presentation.effective_context_tokens, Some(120_000));
        assert_eq!(presentation.maximum_context_tokens, Some(200_000));
        assert_eq!(presentation.maximum_output_tokens, Some(16_000));
        assert_eq!(
            presentation.reasoning_summary,
            ReasoningSummarySupport::Unknown
        );
        assert_eq!(
            presentation.thinking_efforts,
            ["small-exact", "balanced-exact"]
        );
        assert_eq!(
            presentation.default_thinking_effort.as_deref(),
            Some("balanced-exact")
        );
        let pricing = presentation.pricing.unwrap();
        assert_eq!(pricing.currency, None, "empty currency must not become USD");
        assert_eq!(pricing.cache_read_per_million, 0.125);
        assert_eq!(pricing.cache_write_per_million, 1.5);
        assert_eq!(presentation.upgrade.unwrap().target_model, "model-v3");
    }

    #[test]
    fn reasoning_summary_tri_state_is_not_collapsed() {
        let mut model = ModelInfo::default();
        assert_eq!(
            model_presentation(&model).reasoning_summary,
            ReasoningSummarySupport::Unknown
        );
        model.supports_reasoning_summary = Some(false);
        assert_eq!(
            model_presentation(&model).reasoning_summary,
            ReasoningSummarySupport::Unsupported
        );
        model.supports_reasoning_summary = Some(true);
        assert_eq!(
            model_presentation(&model).reasoning_summary,
            ReasoningSummarySupport::Supported
        );
    }

    #[test]
    fn model_search_includes_privacy_effort_currency_and_upgrade_without_guessing() {
        let models = vec![
            ModelInfo {
                id: "private".into(),
                description: "May train on submitted prompts".into(),
                thinking_levels: vec!["provider-special".into()],
                ..ModelInfo::default()
            },
            ModelInfo {
                id: "priced".into(),
                pricing: Some(ModelPricing {
                    currency: "EUR".into(),
                    ..ModelPricing::default()
                }),
                upgrade: Some(ModelUpgrade {
                    model: "next-model".into(),
                    message: "Migration available".into(),
                }),
                ..ModelInfo::default()
            },
        ];
        assert_eq!(search_models(&models, "train"), [0]);
        assert_eq!(search_models(&models, "provider-special"), [0]);
        assert_eq!(search_models(&models, "eur"), [1]);
        assert_eq!(search_models(&models, "next-model"), [1]);
        assert_eq!(model_label(&models[0]), "private");
    }
}
