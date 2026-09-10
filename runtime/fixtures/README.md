# Operational fixtures

Frozen coding/context test inputs and validators used by HaloClu's benchmark
operations live in `suite/`. They were relocated from the historical
STRIX-PRODUCT-001 suite; JSON path metadata and corresponding application pins
were updated. Task source and test payloads were preserved.

`historical-109-256.json` retains the exact historical request payload from
GLM-CIRU-OPT-004, including the absence of an explicit reasoning setting.
Do not reinterpret it as the current interactive profile.

These fixtures remove a live dependency on the retired research repositories.
They contain test problems and local validators, not model weights, credentials
or a representative general-intelligence benchmark. The directory named
`private` holds the validator side of each task, excluded from the model prompt;
it is not a confidential secret or an externally held-out benchmark.

Generated run evidence belongs in ignored state/report directories. Changing a
fixture requires deliberate review and new matching pins; do not relax checks
to make modified fixtures pass.
