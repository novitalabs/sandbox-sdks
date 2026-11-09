# Security policy

Report vulnerabilities privately using the repository's GitHub private security
advisory form. If private reporting is unavailable, contact Novita through its
[official support channels](https://novita.ai). Do not put credentials or an
unredacted exploit involving customer data in a public issue.

Include the SDK language/version, affected operation, expected and observed
behavior, and a minimal reproduction using test resources. Security fixes target
the latest maintained release; prerelease fixes land on alpha before publication.

NOVITA_API_KEY and NOVITA_ACCESS_TOKEN grant access to account resources. Supply them
through the execution environment. Fork pull requests run only offline tests and
must not receive API or publishing secrets. Live acceptance is opt-in and can
consume account quota; use dedicated test resources and verify cleanup.
