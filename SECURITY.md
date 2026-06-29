# Security Policy

## Reporting a Vulnerability

The proto2type team takes security vulnerabilities seriously. We appreciate your efforts to responsibly disclose your findings.

**Please do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of the following methods:

### Option 1: GitHub Security Advisories (Preferred)

Use [GitHub Security Advisories](https://github.com/protocgen/proto2type/security/advisories/new) to privately report a vulnerability. This allows us to collaborate on a fix before public disclosure.

### Option 2: Email

Send an email to **security@protocgen.dev** with the following information:

- A description of the vulnerability
- Steps to reproduce the issue
- Affected versions
- Any potential impact assessment
- Suggested fix (if available)

## Response Timeline

- **Acknowledgement:** We will acknowledge receipt of your report within **48 hours**.
- **Assessment:** We will provide an initial assessment within **5 business days**.
- **Resolution:** We aim to release a fix within **30 days** of confirming the vulnerability, depending on complexity.

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Disclosure Policy

- We follow [coordinated vulnerability disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure).
- We will credit reporters in the security advisory (unless anonymity is requested).
- We ask that you give us reasonable time to address the issue before public disclosure.

## Scope

This policy applies to the proto2type protoc plugin and its generated code. Issues in dependencies should be reported to the respective upstream projects.

## Security Considerations

proto2type is a code generator. Security concerns fall into two categories:

### Generator Security
- **Code injection:** Proto field names are validated against language keywords and sanitized
- **DoS:** Recursive message generation is bounded by protoc's own nesting limits
- **Supply chain:** Generator dependencies are tracked via `go.sum`

### Generated Code Security
- **SQL injection:** Not applicable — generated code uses parameterized rusqlite accessors
- **JSON deserialization:** Uses serde_json defaults (128-level recursion limit)
- **Error handling:** All fallible operations use `Result` with `?` propagation
- **Timestamp handling:** Out-of-range timestamps return `ConversionError::InvalidTimestamp`

See [CONFIG.md](CONFIG.md) for the full trust model documentation.

Thank you for helping keep proto2type and its users safe.
