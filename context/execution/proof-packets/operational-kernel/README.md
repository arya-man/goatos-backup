# Operational Kernel Proof Packets

Each implementation root batch adds one `<batch-id>.md` packet here. The packet
records the base SHA, stable IDs, failing/absent behavior, canonical invariant,
production paths, commands, artifact hashes, recovery, limitations, internal
commit range, and independent-review disposition.

Do not put the packet's own commit SHA inside the packet. F0 defines the
machine-readable SHA-keyed local-CI/review receipt and the single PR proof index
that bind an exact candidate commit to these committed inputs. A prose packet or
checked box does not authorize closure or landing.
