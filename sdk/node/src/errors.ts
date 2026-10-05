/** Local errors never include peer frames, stderr, process arguments, or env. */
export class SdkError extends Error {
  readonly code: string;
  readonly outcomeUnknown: boolean;

  constructor(code: string, message: string, options: { outcomeUnknown?: boolean } = {}) {
    super(message);
    this.name = 'SdkError';
    this.code = code;
    this.outcomeUnknown = options.outcomeUnknown ?? false;
  }
}

/** Remote diagnostics are untrusted; explicitly inspect them, never log implicitly. */
export class RemoteRpcError extends SdkError {
  readonly response: Readonly<Record<string, unknown>>;
  readonly id: string;
  readonly command: string;
  readonly success: false = false;
  readonly error: string | undefined;
  readonly errorCode: string | undefined;
  readonly data: unknown;

  constructor(response: Record<string, unknown>) {
    super('remote_rpc', 'Snow rejected the RPC request.');
    this.name = 'RemoteRpcError';
    this.response = Object.freeze({ ...response });
    this.id = typeof response.id === 'string' ? response.id : '';
    this.command = typeof response.command === 'string' ? response.command : '';
    this.error = typeof response.error === 'string' ? response.error : undefined;
    this.errorCode = typeof response.error_code === 'string' ? response.error_code : undefined;
    this.data = response.data;
  }
}
