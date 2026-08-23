# SDRainer Documentation

SDRainer is an experimental CW (Morse) skimmer: it detects and decodes CW
signals from an audio source or from an SDR IQ stream, extracts callsigns, and
publishes them as spots (telnet DX-cluster interface, TCI spectrum markers).

This folder collects knowledge about the project while it is stabilized. It has
three purposes:

1. **Document the current implementation** — how the code works *today*,
   including the rough edges and experimental parts, so behavior is understood
   before it is changed.
2. **Document the theoretical foundations** — the DSP and detection theory the
   implementation rests on (Goertzel tone detection, FFT/PSD, peak detection,
   debouncing, CW timing), independent of any one piece of code.
3. **Document and develop the architecture** — the intended structure, the
   boundaries between packages, and the direction the design should move as it
   is stabilized.

> **Status: highly experimental.** Treat everything here as a working
> description of a moving target, not a specification. Where the code and the
> intended design disagree, the docs should say so explicitly rather than paper
> over it.

## Documents

- **[The Architecture of SDRainer](./architecture.md) — start here.** One
  document that holds the purpose of the application, each interface, each
  component, each decision, and each way that we tried and did not keep. It
  points to no other document.
- [Research: CW Multi-Signal Decode Pipeline](./research_pipeline.md) — the
  general design of a pipeline that finds and decodes many CW signals at the
  same time. It is theory and it is not a description of the code. The
  architecture document holds what the code does.
- [Glossary](./glossary.md) — the terms of DSP and of CW.
- [Concept: the quality tag of a spot](./spot_quality_concept.md) — the CT1BOH
  tags of AR-Cluster 6, and how one skimmer can give that answer from its own
  evidence.



## Conventions

- One topic per Markdown file, `snake_case` filenames.
- Diagrams in [Mermaid](https://mermaid.js.org/).
- Link code as `package/file.go` and name the relevant type/function, so a
  claim can be checked against the source.
- Prefer marking an uncertainty explicitly over stating a confident guess.
- Document files ending in `_goal.md` describe the intended goal. 
- Document files ending in `_plan.md` describe an implementation plan.
- Other files describe the current state, a previous state, or general information (for example a glossary).
