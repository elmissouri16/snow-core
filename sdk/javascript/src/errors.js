export class SnowError extends Error {
  constructor(message) {
    super(message);
    this.name = new.target.name;
  }
}
export class SnowClosedError extends SnowError {}
export class SnowProcessError extends SnowError {}
export class SnowProtocolError extends SnowError {}
export class SnowVersionError extends SnowProtocolError {}
export class SnowTimeoutError extends SnowError {}
export class SnowCancelledError extends SnowError {}
export class SnowSubscriptionOverflowError extends SnowError {}

export class SnowCommandError extends SnowError {
  constructor(command, requestId, message, response) {
    super(`${command} (${requestId}): ${message}`);
    this.command = command;
    this.requestId = requestId;
    this.errorCode = typeof response?.error_code === "string" ? response.error_code : undefined;
    this.response = structuredClone(response);
  }
}

export class SnowPromptError extends SnowError {
  constructor(requestId, message) {
    super(`prompt (${requestId}): ${message}`);
    this.requestId = requestId;
  }
}
