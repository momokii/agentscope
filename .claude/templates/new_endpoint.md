# New API Endpoint Checklist

## Before Starting
- [ ] Endpoint defined in API contract (if one exists); method, route, behavior clear
- [ ] Auth and permission requirements understood
- [ ] Active environment identified (development)

## Implementation
- [ ] Route registered following existing patterns; handler follows existing patterns
- [ ] Input validated with a schema before any logic runs; unknown fields handled safely
- [ ] Business logic separated from the handler
- [ ] Correct status codes; errors use the project's standard envelope
- [ ] No internal details or stack traces in error responses
- [ ] Rate limiting / size limits considered; caching consistent with existing patterns

## Security Gate (see SECURITY_STANDARDS.md)
- [ ] Authentication and authorization enforced on this route; object-level access checked
- [ ] No secrets or sensitive data in logs or responses
- [ ] No over-exposure of fields in responses
- [ ] New dependencies verified, audited, pinned, logged
- [ ] Verified under development AND production configuration

## Testing
- [ ] Happy path; validation failures; unauthenticated; wrong-role; oversized/malformed input
- [ ] All tests, lint, and type checks pass

## Completion
- [ ] API contract documentation updated
- [ ] TASK_QUEUE.md and CURRENT_STATUS.md updated
