# No internal implementation details

This repository is public. Do not let a change publish how SonarQube Cloud
or SonarQube Server works inside.

Flag each added or changed line that does one of these things. Check Markdown
files, code comments, test comments, commit messages, and the PR description.

- It describes the behavior of an internal endpoint in more detail than the
  client needs. Examples are the reason for a status code, the content of an
  error response, or the effect of a hidden parameter.
- It states a server limit, a timing, a retry delay, or a consistency
  behavior that the author learned from a call on a live instance or from the
  server source code.
- It names an internal service, a host, a queue, a flag, a table, or an
  identifier that the public documentation does not name.
- It copies a value from a live instance or from a captured API response.

Do not flag these items:

- A link to a public API documentation page.
- A short note that tells why the provider calls an API in a given way, such
  as the identifier that the API takes, why an internal or Web API v1
  endpoint is in use, or a limit or semantic that shapes a resource.
- The path or the fields of an endpoint that the client calls, in `APIS.md`
  or in the client code.
- A status code or an error text that the client code branches on, when a
  comment next to that code states it.

For each finding, give the file and the line, and say which of the cases above
applies. Tell the author to ask before the change keeps or removes the text.
Do not rewrite the text. The author or the maintainers decide what is safe to
publish.

See "Data sensitivity" and "Keep the API inventory current" in `AGENTS.md`.
