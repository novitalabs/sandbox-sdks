# Go SDK runtime compatibility

The SDK negotiates capabilities with `envd`. Each operation uses the corresponding
runtime feature when the running sandbox supports it.

| Feature | Minimum envd |
|---|---|
| Recursive directory watch | 0.1.4 |
| Command stdin | 0.3.0 |
| Multi-user support | 0.4.0 |
| `SendInput` / close | 0.5.2 |
| Octet-stream upload | 0.5.7 |

Older runtimes use streamed multipart uploads when octet-stream upload is not
available.
