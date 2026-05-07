# Design

The full design for `askdao-cli` lives upstream in the AskDAO design archive:

- [`harness-design/designs/askdao-cli-environment-bootstrap.md`](https://github.com/askdao/harness-design/blob/main/designs/askdao-cli-environment-bootstrap.md)

That document covers:

- The four-layer scanning pipeline (syft → dev-filter → providers → LLM)
- `detection.json` and `agent.yml` schemas
- Phase 1 MVP scope and engineering estimates
- Open decisions pending review

Two upstream spike reports informed the design:

- [`investigations/syft-spike-for-askdao-cli.md`](https://github.com/askdao/harness-design/blob/main/investigations/syft-spike-for-askdao-cli.md)
- [`investigations/nixpacks-provider-pattern.md`](https://github.com/askdao/harness-design/blob/main/investigations/nixpacks-provider-pattern.md)

> Note: while this repository is private, those documents may also be private. Once `askdao-cli` ships its first public release, the relevant portions will be inlined here.
