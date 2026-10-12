"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.redactSensitiveText = redactSensitiveText;
exports.redactSensitiveData = redactSensitiveData;
/**
 * Best-effort removal of common credential shapes from returned text. It is a
 * second line of defence, not a guarantee: a page can show a secret in a form
 * no pattern recognises, which is why authenticated results require explicit
 * acknowledgement that they may contain private content.
 */
const SECRET_NAME = '(?:access[_-]?token|refresh[_-]?token|id[_-]?token|api[_-]?key|api[_-]?token|auth(?:orization)?|client[_-]?secret|jwt|password|passwd|secret|session(?:[_-]?id)?|sid|token)';
const QUERY_SECRET = new RegExp(`([?&](?:x[_-])?${SECRET_NAME}=[^&#\\s]*)`, 'gi');
const ASSIGNED_SECRET = new RegExp(`\\b((?:x[_-])?${SECRET_NAME}[A-Za-z0-9_-]*)=([^\\s&]+)`, 'gi');
function redactSensitiveText(value) {
    return value
        .replace(/(https?:\/\/)[^\s/@]+@/gi, '$1[REDACTED]@')
        .replace(QUERY_SECRET, (match) => {
        const separator = match.indexOf('=');
        return `${match.slice(0, separator + 1)}[REDACTED]`;
    })
        .replace(ASSIGNED_SECRET, '$1=[REDACTED]')
        .replace(/\b(Bearer|Basic)\s+[^\s"']+/gi, '$1 [REDACTED]')
        // A JSON Web Token: three base64url segments, the first two starting with eyJ.
        .replace(/\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*/g, '[REDACTED]');
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
