#!/usr/bin/env python3
"""Derive one row per backend domain module: what it owns, emits, consumes, and whether it is tested.

Everything printed is DERIVED from files this script actually READ. Nothing is taken from a
module's own prose. Counters below count reads, never listings: a file that could not be read is
an error, never a zero (automation-handover.md §3 trap 6).

Run:  python3 tools/module-map/derive-module-map.py [--json out.json]
"""
import json, os, re, sys, collections

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
INTERNAL = os.path.join(ROOT, "backend", "internal")
REGISTRY = os.path.join(ROOT, "context", "architecture", "domain-event-registry.json")

BRIDGES = {"countsbridge","sopbridge","notificationbridge","domainconsumer","eventwiring","kernelstages"}
# Not a domain module: shared libraries with no business state of their own.
INFRA = {"platform"}

read_ok = 0
read_paths = set()
read_err = []

def read(p):
    global read_ok
    try:
        with open(p, "r", encoding="utf-8", errors="replace") as f:
            s = f.read()
        read_ok += 1
        read_paths.add(p)
        return s
    except OSError as e:
        read_err.append((p, str(e)))
        return None

# ---------- module discovery ----------
modules = sorted(d for d in os.listdir(INTERNAL) if os.path.isdir(os.path.join(INTERNAL, d)))

def mod_of(path):
    """Map a repo-relative path to the owning backend module, or None."""
    p = path.replace("\\", "/")
    i = p.find("backend/internal/")
    if i < 0:
        return None
    rest = p[i + len("backend/internal/"):]
    return rest.split("/")[0] if rest else None

# ---------- walk each module's go files ----------
WRITE_RE = re.compile(r"\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?([a-z_][a-z0-9_]*)", re.I)
READ_RE  = re.compile(r"\b(?:FROM|JOIN)\s+(?:ONLY\s+)?([a-z_][a-z0-9_]*)", re.I)
SUB_RE   = re.compile(r"\.Subscribe\(\s*([A-Za-z0-9_.\"][^,]*?)\s*,")
PUB_RE   = re.compile(r"(?:EventType|Type):\s*([A-Za-z0-9_.\"][^,\n}]*)")
CONST_RE = re.compile(r'\b([eE]vent[A-Za-z0-9_]+)\s*=\s*"([a-z0-9_.]+)"')
ROUTE_RE = re.compile(r'Pattern:\s*"([^"]+)"')
IMPORT_RE = re.compile(r'"github\.com/vgoats/goatos/backend/internal/([a-z_0-9]+)')

SQL_NOISE = {"select","values","dual","only","lateral","unnest","generate_series","jsonb_array_elements",
             "jsonb_array_elements_text","json_array_elements","set","where","as","table"}

mod_files = collections.defaultdict(list)
for m in modules:
    base = os.path.join(INTERNAL, m)
    for dp, dn, fn in os.walk(base):
        dn[:] = [d for d in dn if d not in (".git", "testdata")]
        for f in fn:
            if f.endswith(".go"):
                mod_files[m].append(os.path.join(dp, f))

# constant table: qualified + bare name -> event string
consts = {}
for m in modules:
    for p in mod_files[m]:
        s = read(p)
        if s is None: continue
        for name, val in CONST_RE.findall(s):
            consts[name] = val
            consts[f"{os.path.basename(os.path.dirname(p))}.{name}"] = val

def resolve_event(tok):
    tok = tok.strip()
    if tok.startswith('"'):
        return tok.strip('"')
    bare = tok.split(".")[-1]
    return consts.get(tok) or consts.get(bare)

data = {}
for m in modules:
    writes, reads, subs, pubs, imps = set(), set(), set(), set(), set()
    tests, srcs, unres = [], 0, set()
    for p in mod_files[m]:
        s = read(p)
        if s is None: continue
        rel = os.path.relpath(p, ROOT)
        if p.endswith("_test.go"):
            tests.append(rel); continue
        srcs += 1
        for t in WRITE_RE.findall(s):
            if t.lower() not in SQL_NOISE: writes.add(t.lower())
        for t in READ_RE.findall(s):
            if t.lower() not in SQL_NOISE: reads.add(t.lower())
        for tok in SUB_RE.findall(s):
            e = resolve_event(tok)
            (subs.add(e) if e else unres.add(tok.strip()))
        for tok in PUB_RE.findall(s):
            e = resolve_event(tok)
            if e: pubs.add(e)
        for im in IMPORT_RE.findall(s):
            if im != m and im in set(modules): imps.add(im)
    data[m] = dict(module=m, go_src_files=srcs, test_files=sorted(tests),
                   writes=sorted(writes), reads=sorted(reads),
                   code_subscribes=sorted(subs), code_publishes=sorted(pubs),
                   unresolved_subscribe_tokens=sorted(unres), imports_modules=sorted(imps))

# ---------- registry ----------
reg = json.loads(read(REGISTRY) or "{}")
events = reg.get("events", [])
reg_emits = collections.defaultdict(set)      # module -> eventType
reg_consumes = collections.defaultdict(set)   # module -> eventType
ev_index = {}
for e in events:
    et = e["eventType"]
    ev_index[et] = e
    for f in e.get("producerFiles", []):
        mm = mod_of(f)
        if mm: reg_emits[mm].add(et)
    for c in e.get("consumers", []):
        mm = mod_of(c.get("file", "")) or c.get("module")
        if mm: reg_consumes[mm].add(et)

# e2e proof files per module
reg_proof = collections.defaultdict(set)
for e in events:
    for f in e.get("e2eProof", []):
        mm = mod_of(f)
        if mm: reg_proof[mm].add(os.path.relpath(f, "") )

# ---------- what each bridge CARRIES ----------
# A bridge wires handlers it does not own: `<pkg>.New<Name>(...).Register(bus)`. The Register
# METHOD lives in the owning module and is what calls bus.Subscribe. So a bridge's couplings are
# invisible to a per-module scan and to any per-screen check -- derive them by following the
# handler type back to its owner.
HANDLER_RE = re.compile(r"\b([a-z][a-z0-9_]*)\.(New[A-Za-z0-9_]+)\s*\(")
REGMETH_RE = re.compile(r"func\s*\(\s*\w+\s+\*?([A-Za-z0-9_]+)\s*\)\s*Register\s*\(")

# index: handler struct name -> (module, events it subscribes to)
handler_events = {}
for mm in modules:
    for pth in mod_files[mm]:
        if pth.endswith("_test.go"): continue
        src = read(pth)
        if src is None: continue
        for match in REGMETH_RE.finditer(src):
            struct = match.group(1)
            body = src[match.end():match.end() + 1500]
            evs = set()
            for tok in SUB_RE.findall(body):
                e = resolve_event(tok)
                if e: evs.add(e)
            if evs:
                prev = handler_events.get(struct, (mm, set()))
                handler_events[struct] = (mm, prev[1] | evs)

bridge_carries = {}
for b in sorted(BRIDGES):
    carried = collections.defaultdict(set)   # owning module -> events
    unmatched = set()
    for pth in mod_files[b]:
        if pth.endswith("_test.go"): continue
        src = read(pth)
        if src is None: continue
        for pkg, ctor in HANDLER_RE.findall(src):
            struct = ctor[3:]
            if struct in handler_events:
                own, evs = handler_events[struct]
                carried[own] |= evs
            elif "Handler" in struct or "Consumer" in struct:
                unmatched.add(f"{pkg}.{ctor}")
    bridge_carries[b] = dict(
        carries={k: sorted(v) for k, v in sorted(carried.items())},
        distinct_events=sorted({e for v in carried.values() for e in v}),
        handlers_wired_whose_events_could_not_be_resolved=sorted(unmatched))

# ---------- routes ----------
routes_txt = read(os.path.join(INTERNAL, "permissions", "routes.go")) or ""
all_routes = ROUTE_RE.findall(routes_txt)

# ---------- assemble rows ----------
rows = []
for m in modules:
    d = data[m]
    emits = sorted(set(d["code_publishes"]) | reg_emits[m])
    consumes = sorted(set(d["code_subscribes"]) | reg_consumes[m])
    # a coupling is registry-backed only when a registry row names this module as producer/consumer
    emits_unregistered = sorted(set(d["code_publishes"]) - reg_emits[m])
    consumes_unregistered = sorted(set(d["code_subscribes"]) - reg_consumes[m])
    # cross-module table reads: tables it reads but never writes, and another module writes
    foreign_reads = sorted(t for t in d["reads"] if t not in d["writes"])
    kind = "bridge" if m in BRIDGES else ("infrastructure" if m in INFRA else "domain")
    imports = [i for i in d["imports_modules"] if i not in INFRA]
    coupled = bool(emits or consumes or imports) or kind == "bridge"
    rows.append(dict(module=m, kind=kind,
                     owns_tables=d["writes"], reads_tables=foreign_reads,
                     emits=emits, consumes=consumes,
                     emits_without_registry_row=emits_unregistered,
                     consumes_without_registry_row=consumes_unregistered,
                     coupling=("coupled" if coupled else "independent"),
                     imports_modules=imports,
                     coupling_evidence=[e for e,v in (("emits",emits),("consumes",consumes),("imports",imports)) if v],
                     go_src_files=d["go_src_files"],
                     test_file_count=len(d["test_files"]),
                     test_files=d["test_files"][:6],
                     registry_e2e_proof=sorted(reg_proof[m]),
                     unresolved_subscribe_tokens=d["unresolved_subscribe_tokens"]))

# writer index: which module writes a table (for coupling evidence)
writer = collections.defaultdict(set)
for r in rows:
    for t in r["owns_tables"]:
        writer[t].add(r["module"])
for r in rows:
    r["reads_tables_owned_elsewhere"] = sorted(
        {f"{t}<-{','.join(sorted(writer[t]-{r['module']}))}" for t in r["reads_tables"]
         if writer[t] - {r["module"]}})

imported_by = collections.defaultdict(set)
for r in rows:
    for i in r["imports_modules"]:
        imported_by[i].add(r["module"])
for r in rows:
    r["imported_by_modules"] = sorted(imported_by[r["module"]])
    if r["coupling"] == "independent" and r["imported_by_modules"]:
        r["coupling"] = "coupled"
        r["coupling_evidence"] = ["imported_by"]

# ---------- event-level integrity ----------
all_sub = {}   # event -> modules subscribing (in code)
all_pub = {}   # event -> modules publishing (in code)
for r in rows:
    for e in data[r["module"]]["code_subscribes"]: all_sub.setdefault(e, set()).add(r["module"])
    for e in data[r["module"]]["code_publishes"]: all_pub.setdefault(e, set()).add(r["module"])
reg_types = set(ev_index)
# Second producer pass. `Type: eventType` passes the type through a VARIABLE (sopbridge's verify
# fan-out does exactly this), which the literal scan cannot follow. So: a module that both names
# the event string and calls .Publish( is recorded as a CANDIDATE producer -- weaker evidence,
# labelled as such, never merged into the literal set.
pub_candidates = collections.defaultdict(set)
for mm in modules:
    for pth in mod_files[mm]:
        if pth.endswith("_test.go"): continue
        src = read(pth)
        if src is None or ".Publish(" not in src: continue
        for e in set(list(all_sub) + list(ev_index)):
            if f'"{e}"' in src or any(f'{n} = "{e}"' in src for n in ()): pub_candidates[e].add(mm)
        for ident in re.findall(r"\b[eE]vent[A-Za-z0-9_]+\b", src):
            v = consts.get(ident)
            if v: pub_candidates[v].add(mm)

# BLIND SPOT, stated rather than rounded away: producers are detected from Go event literals only.
# Two counts events are produced by an INSERT into outbox_messages inside a SQL transaction
# (confirmed by their partial unique indexes in migration 000001), so they read here as
# producer-less. A missing producer-detector pushes this list LONGER than the truth, never shorter.
integrity = dict(
    subscribed_in_code_but_absent_from_registry=sorted(
        {e: sorted(m) for e, m in all_sub.items() if e not in reg_types}.items()),
    subscribed_in_code_but_no_producer_anywhere=sorted(
        {e: sorted(m) for e, m in all_sub.items()
         if e not in all_pub and not (ev_index.get(e, {}).get("producerFiles"))
         and not (pub_candidates.get(e, set()) - m)}.items()),
    producer_only_by_string_candidate={e: sorted(v) for e, v in sorted(pub_candidates.items())
         if e not in all_pub and not ev_index.get(e, {}).get("producerFiles")},
    registry_events_with_no_consumer=sorted(e for e in reg_types if not ev_index[e].get("consumers")),
)

out = dict(
    derived_from=dict(root=ROOT, distinct_go_files_read=len(read_paths), file_read_calls=read_ok, read_errors=read_err,
                      registry_events=len(events),
                      registry_events_with_consumer=sum(1 for e in events if e.get("consumers")),
                      route_patterns_in_permissions_routes_go=len(set(all_routes)),
                      modules_found=len(modules)),
    bridge_carries=bridge_carries,
    event_integrity=integrity,
    rows=rows)

if "--json" in sys.argv:
    p = sys.argv[sys.argv.index("--json") + 1]
    with open(p, "w") as f: json.dump(out, f, indent=1)

m = out["derived_from"]
print(f"DISTINCT go files READ: {m['distinct_go_files_read']}  (read calls: {m['file_read_calls']})  read errors: {len(read_err)}")
if read_err:
    print("  !! unread inputs are errors, not zeros:", read_err[:5])
print(f"modules: {m['modules_found']}   registry events: {m['registry_events']} "
      f"({m['registry_events_with_consumer']} with >=1 consumer)")
print(f"route patterns: {m['route_patterns_in_permissions_routes_go']}")
ind = [r['module'] for r in rows if r['coupling']=='independent']
print(f"coupled: {len(rows)-len(ind)}   independent: {len(ind)} -> {', '.join(ind)}")
unreg_c = [(r['module'], r['consumes_without_registry_row']) for r in rows if r['consumes_without_registry_row']]
print(f"\nmodules consuming events with NO registry row naming them: {len(unreg_c)}")
for mm, evs in unreg_c:
    print(f"  {mm}: {', '.join(evs)}")
print("\nWHAT THE SIX BRIDGE MODULES CARRY (couplings no per-module or per-screen check can see):")
for b, v in bridge_carries.items():
    print(f"  {b}: {len(v['distinct_events'])} distinct events for {len(v['carries'])} modules"
          f" -> {', '.join(v['carries']) or '(none)'}")
    if v['handlers_wired_whose_events_could_not_be_resolved']:
        print(f"     UNRESOLVED (not zero -- unread): {', '.join(v['handlers_wired_whose_events_could_not_be_resolved'])}")

print("\nEVENT INTEGRITY (derived from code that was READ, not from the registry's own claims):")
print(f"  subscribed in code, NO registry row at all: {len(integrity['subscribed_in_code_but_absent_from_registry'])}")
for e, m in integrity['subscribed_in_code_but_absent_from_registry']:
    print(f"    {e}  <- consumed by {', '.join(m)}")
print(f"  subscribed in code, NO producer found anywhere: {len(integrity['subscribed_in_code_but_no_producer_anywhere'])}")
for e, m in integrity['subscribed_in_code_but_no_producer_anywhere']:
    print(f"    {e}  <- consumed by {', '.join(m)}")
print(f"  registry events with zero consumers: {len(integrity['registry_events_with_no_consumer'])}")

untested = [r['module'] for r in rows if r['test_file_count']==0]
print(f"\nmodules with zero *_test.go: {len(untested)} -> {', '.join(untested) or '(none)'}")
