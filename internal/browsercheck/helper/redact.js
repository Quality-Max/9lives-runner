"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.redactSensitiveText = redactSensitiveText;
exports.redactSensitiveData = redactSensitiveData;
function redactSensitiveText(value) {
    return value
        .replace(/(https?:\/\/)[^\s/@]+@/gi, '$1[REDACTED]@')
        .replace(/([?&](?:access[_-]?token|api[_-]?key|auth(?:orization)?|client[_-]?secret|password|secret|token)=[^&#\s]*)/gi, (match) => {
        const separator = match.indexOf('=');
        return `${match.slice(0, separator + 1)}[REDACTED]`;
    })
        .replace(/\b((?:access[_-]?token|api[_-]?key|auth(?:orization)?|client[_-]?secret|password|secret|token)[A-Za-z0-9_-]*)=([^\s&]+)/gi, '$1=[REDACTED]')
        .replace(/\b(Bearer)\s+[^\s"']+/gi, '$1 [REDACTED]');
}
/** Recursively redact returned page data without changing its shape. */
function redactSensitiveData(value) {
    if (typeof value === 'string')
        return redactSensitiveText(value);
    if (Array.isArray(value))
        return value.map(redactSensitiveData);
    if (value && typeof value === 'object') {
        return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, redactSensitiveData(item)]));
    }
    return value;
}
