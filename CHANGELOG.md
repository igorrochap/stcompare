# CHANGELOG

<!-- version list -->

## v2.1.0 (2026-09-16)

### Bug Fixes

- **bench**: Prevent stdout pipe race condition
  ([`0421e3e`](https://github.com/igorrochap/stcompare/commit/0421e3e85159f3ff4c952b98afd8b2e7b0bf988c))

### Features

- **agent**: Group actionable problems in schema v2
  ([#108](https://github.com/igorrochap/stcompare/pull/108),
  [`32efc39`](https://github.com/igorrochap/stcompare/commit/32efc3971040a5750952722d89e44b2b027fb1a0))

- **bench**: Add prompt size limit guard ([#108](https://github.com/igorrochap/stcompare/pull/108),
  [`32efc39`](https://github.com/igorrochap/stcompare/commit/32efc3971040a5750952722d89e44b2b027fb1a0))

- **bench**: Update benchmark record to version 2
  ([#108](https://github.com/igorrochap/stcompare/pull/108),
  [`32efc39`](https://github.com/igorrochap/stcompare/commit/32efc3971040a5750952722d89e44b2b027fb1a0))

- **bench**: Upgrade benchmark schema to v2 and add prompt constraints
  ([#108](https://github.com/igorrochap/stcompare/pull/108),
  [`32efc39`](https://github.com/igorrochap/stcompare/commit/32efc3971040a5750952722d89e44b2b027fb1a0))


## v2.0.0 (2026-09-14)

### Features

- **docs**: Introduce problem groups for agent view
  ([`c1bc645`](https://github.com/igorrochap/stcompare/commit/c1bc645657cf74bc056f4eb9e448bf9445ffc374))


## v1.8.0 (2026-09-14)

### Features

- **comparison**: Add custom check replay oracles
  ([#101](https://github.com/igorrochap/stcompare/pull/101),
  [`1e3fff7`](https://github.com/igorrochap/stcompare/commit/1e3fff7f378dc87e310e7eecce48777ddeace7c2))


## v1.7.0 (2026-09-13)

### Features

- **comparison**: Add support for new Schemathesis check categories
  ([#100](https://github.com/igorrochap/stcompare/pull/100),
  [`25b830d`](https://github.com/igorrochap/stcompare/commit/25b830d3a62bdd5f56c65b049a435bbe3d11be77))


## v1.6.0 (2026-09-13)

### Bug Fixes

- **ci**: Adjust quality checks for import-only changes
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

### Features

- **audit**: Add tool call and adapter operation activity tracking
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Capture and report final source evidence
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Collapse JSON payloads in HTML reports
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Implement comprehensive model turn audit system
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Implement focused source diffing
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Implement local-model turn audit system
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Implement model turn efficiency and overhead tracking
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **audit**: Track file modifications and edit attempts
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

### Refactoring

- **audit**: Modularize source code ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **bench**: Extract helper functions for command and run logic
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))

- **ui**: Extract HTML templates to separate files
  ([#99](https://github.com/igorrochap/stcompare/pull/99),
  [`743c05f`](https://github.com/igorrochap/stcompare/commit/743c05f000db78440f049a5e8096db5ac67a6ff5))


## v1.5.0 (2026-09-08)


## v1.4.0 (2026-08-28)

### Features

- **adapter**: Add tuning options for local models
  ([`0a7fe40`](https://github.com/igorrochap/stcompare/commit/0a7fe4095810dbafecb746a3095a2fb3f5034d71))

### Refactoring

- **stbench**: Remove run_command tool from adapter
  ([#75](https://github.com/igorrochap/stcompare/pull/75),
  [`6f54f14`](https://github.com/igorrochap/stcompare/commit/6f54f143ec569b08dff121e5ebbc515a49c61dcf))


## v1.3.0 (2026-08-26)

### Features

- **adapter**: Add str_replace tool and history compaction
  ([#74](https://github.com/igorrochap/stcompare/pull/74),
  [`c45cd75`](https://github.com/igorrochap/stcompare/commit/c45cd758cf96eb0b7c73ccfa53b297ea2f714b69))


## v1.2.0 (2026-08-26)

### Bug Fixes

- **test**: Increase command timeout in benchmark
  ([#73](https://github.com/igorrochap/stcompare/pull/73),
  [`70d4da4`](https://github.com/igorrochap/stcompare/commit/70d4da4d8c2d13ec473653160ea3dea6057d844a))

### Features

- **bench**: Add support for sampling temperature
  ([#73](https://github.com/igorrochap/stcompare/pull/73),
  [`70d4da4`](https://github.com/igorrochap/stcompare/commit/70d4da4d8c2d13ec473653160ea3dea6057d844a))


## v1.1.0 (2026-08-25)

### Bug Fixes

- **ci**: Pin GitPython version for semantic release
  ([`d932901`](https://github.com/igorrochap/stcompare/commit/d932901ab00bfccd8f2aaacb741e4b9e0756990a))

### Chores

- **ci**: Restrict build job to main branch
  ([`a14f934`](https://github.com/igorrochap/stcompare/commit/a14f934b94515bba5d81aa9e045e036175a2dcbe))

- **git**: Ignore .syl configuration files ([#69](https://github.com/igorrochap/stcompare/pull/69),
  [`f25d005`](https://github.com/igorrochap/stcompare/commit/f25d005a1bfc8eade76d8d778f185e1fe824583e))

- **git**: Ignore .syl configuration files
  ([`61cee45`](https://github.com/igorrochap/stcompare/commit/61cee4592aa3af75492bac89ea659e94dc5812ef))

### Documentation

- **adr**: Add ADR for external prompt file override
  ([`7adf37d`](https://github.com/igorrochap/stcompare/commit/7adf37d19688b471adb755303ce31e41df427c8a))

- **readme**: Update stbench documentation and quick start
  ([`57f00c1`](https://github.com/igorrochap/stcompare/commit/57f00c15884f7e171223b5e46eb6fbd95d815479))

### Features

- **bench**: Support custom task prompt templates
  ([#69](https://github.com/igorrochap/stcompare/pull/69),
  [`f25d005`](https://github.com/igorrochap/stcompare/commit/f25d005a1bfc8eade76d8d778f185e1fe824583e))


## v1.0.0 (2026-08-13)

- Initial Release
