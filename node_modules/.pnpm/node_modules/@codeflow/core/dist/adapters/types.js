/**
 * CLI Adapter 接口类型定义
 */
/**
 * API 错误类型
 */
export class APIError extends Error {
    constructor(message, statusCode, code, retryable = false) {
        super(message);
        this.statusCode = statusCode;
        this.code = code;
        this.retryable = retryable;
        this.name = 'APIError';
    }
}
/**
 * 超时错误
 */
export class TimeoutError extends Error {
    constructor(message = 'Request timeout') {
        super(message);
        this.name = 'TimeoutError';
    }
}
//# sourceMappingURL=types.js.map