/** Error thrown for non-2xx daemon HTTP responses. */
export class GrainAPIError extends Error {
  readonly status: number;
  readonly body?: string;

  constructor(status: number, message: string, body?: string) {
    super(message);
    this.name = "GrainAPIError";
    this.status = status;
    this.body = body;
  }
}
