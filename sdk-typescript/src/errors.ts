export class RatError extends Error {
  public readonly statusCode: number | null;

  constructor(message: string, statusCode: number | null = null) {
    super(message);
    this.name = "RatError";
    this.statusCode = statusCode;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class AuthenticationError extends RatError {
  constructor(message: string = "Authentication required") {
    super(message, 401);
    this.name = "AuthenticationError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class AuthorizationError extends RatError {
  constructor(message: string = "Insufficient permissions") {
    super(message, 403);
    this.name = "AuthorizationError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class NotFoundError extends RatError {
  constructor(message: string = "Resource not found") {
    super(message, 404);
    this.name = "NotFoundError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ConflictError extends RatError {
  constructor(message: string = "Resource conflict") {
    super(message, 409);
    this.name = "ConflictError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ValidationError extends RatError {
  public readonly details: unknown;

  constructor(message: string = "Validation error", details?: unknown) {
    super(message, 422);
    this.name = "ValidationError";
    this.details = details;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ServerError extends RatError {
  constructor(
    message: string = "Internal server error",
    statusCode: number = 500,
  ) {
    super(message, statusCode);
    this.name = "ServerError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ConnectionError extends RatError {
  constructor(message: string = "Connection failed") {
    super(message, null);
    this.name = "ConnectionError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

const STATUS_CODE_MAP: Record<
  number,
  new (message: string) => RatError
> = {
  401: AuthenticationError,
  403: AuthorizationError,
  404: NotFoundError,
  409: ConflictError,
  422: ValidationError,
};

export function errorFromStatus(
  statusCode: number,
  message: string,
  details?: unknown,
): RatError {
  if (statusCode === 422) return new ValidationError(message, details);
  const ErrorClass = STATUS_CODE_MAP[statusCode];
  if (ErrorClass) return new ErrorClass(message);
  if (statusCode >= 500) return new ServerError(message, statusCode);
  return new RatError(message, statusCode);
}
