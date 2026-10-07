# Offline Tier 1 process benchmark

One moved-ID offline proposal per fresh process. Python includes isolated startup, imports, source-only compilation and pinned provenance bootstrap/checks; Go includes startup and native proposal. Build and coordinator identity checks excluded. No browser, provider, verification or language ratio.

First recorded invocation per engine; OS/filesystem cache state is uncontrolled. Subsequent samples also use fresh processes.

30 invocations per engine; seeded interleaving (2151).

| Engine | First ms | Subsequent median ms | Min ms | Max ms |
|---|---:|---:|---:|---:|
| python_0.1.3 | 232.335 | 221.740 | 210.777 | 772.579 |
| python_0.2.1 | 214.730 | 232.160 | 213.810 | 568.542 |
| go_native | 330.623 | 5.354 | 4.784 | 6.831 |

The adjacent JSON contains each raw fixture response, order, timing, actual imported-module digests, all Go source inputs, exact patch identity, compiler/target/binary architecture and matching pre/post provenance.
