#!/usr/bin/env python3
"""Assemble the goatos-docs Graphify graph from extracted fragments.

Shared by both the auto-rebuild (rebuild-docs-graph.sh) and a one-time full
rebuild. Takes a directory of extraction fragments (chunk_*.json, each a
{"nodes":[...],"edges":[...],"hyperedges":[...]} object), merges them, clusters,
regenerates graph.json / graph.html / GRAPH_REPORT.md / manifest.json into the
canonical graphify-out, and stamps the origin/main SHA the graph was built from.

All source_file paths are normalized to be RELATIVE to the main worktree root, so
the graph is portable across checkouts (the old absolute-path manifest broke
hash-diff whenever a different worktree ran the rebuild).

Modes:
  full         nodes come entirely from the fresh fragments (default).
  incremental  start from prev graph, drop nodes whose source_file changed or was
               deleted, then splice in the fresh fragments for changed files.

Exit 0 on success (writes outputs + basis stamp). Exit 2 on failure (leaves
outputs untouched so the caller can restore/keep a marker).
"""
import argparse
import hashlib
import json
import os
import sys
from pathlib import Path

from graphify.build import build_from_json
from graphify.cluster import cluster, score_all
from graphify.analyze import god_nodes, surprising_connections, suggest_questions
from graphify.report import generate
from graphify.export import to_json, to_html


def rel_to_main(sf, main_root):
    if not sf:
        return sf
    if os.path.isabs(sf):
        try:
            return os.path.relpath(sf, main_root)
        except ValueError:
            return sf
    return sf


def load_fragments(frag_dir):
    nodes, edges, hyper = [], [], []
    errors = []
    frag_dir = Path(frag_dir)
    chunk_json = sorted(frag_dir.glob("chunk_*.json"))
    for path in chunk_json:
        try:
            data = json.loads(path.read_text())
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{path.name}: invalid json: {exc}")
            continue
        if not isinstance(data, dict):
            errors.append(f"{path.name}: top-level is not an object")
            continue
        nodes.extend(data.get("nodes", []) or [])
        edges.extend(data.get("edges", []) or [])
        hyper.extend(data.get("hyperedges", []) or [])
    # every chunk_*.txt must have produced a chunk_*.json
    expected = {p.with_suffix(".json").name for p in frag_dir.glob("chunk_*.txt")}
    actual = {p.name for p in chunk_json}
    missing = sorted(expected - actual)
    if missing:
        errors.append("missing extraction fragments: " + ", ".join(missing[:10]))
    return nodes, edges, hyper, errors


def normalize_node(n, main_root):
    n["source_file"] = rel_to_main(n.get("source_file"), main_root)
    if not n.get("file_type"):
        n["file_type"] = "document"
    for f in ("source_location", "source_url", "captured_at", "author", "contributor"):
        n.setdefault(f, None)
    return n


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--fragments-dir", required=True)
    ap.add_argument("--corpus-file", required=True, help="abs paths in main worktree")
    ap.add_argument("--main-root", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--basis", required=True, help="origin/main SHA")
    ap.add_argument("--mode", choices=["full", "incremental"], default="full")
    ap.add_argument("--changed-file", default=None, help="rel paths changed (incremental)")
    ap.add_argument("--deleted-file", default=None, help="rel paths deleted (incremental)")
    ap.add_argument("--min-nodes", type=int, default=1)
    args = ap.parse_args()

    out = args.out
    main_root = os.path.abspath(args.main_root)
    corpus_abs = [l.strip() for l in open(args.corpus_file) if l.strip()]
    corpus_rel = [rel_to_main(p, main_root) for p in corpus_abs]
    corpus_rel_set = set(corpus_rel)

    frag_nodes, frag_edges, frag_hyper, errs = load_fragments(args.fragments_dir)
    if errs:
        print("ASSEMBLE_FAILED " + "; ".join(errs[:10]))
        raise SystemExit(2)
    for n in frag_nodes:
        normalize_node(n, main_root)

    if args.mode == "incremental":
        prev_path = f"{out}/graph.json"
        g = json.load(open(prev_path)) if os.path.exists(prev_path) else \
            {"nodes": [], "links": [], "edges": [], "hyperedges": []}
        changed = set()
        deleted = set()
        if args.changed_file and os.path.exists(args.changed_file):
            changed = {rel_to_main(l.strip(), main_root)
                       for l in open(args.changed_file) if l.strip()}
        if args.deleted_file and os.path.exists(args.deleted_file):
            deleted = {rel_to_main(l.strip(), main_root)
                       for l in open(args.deleted_file) if l.strip()}
        touched = changed | deleted
        prev_nodes = g.get("nodes", [])
        prev_count = len(prev_nodes)
        kept = [n for n in prev_nodes
                if rel_to_main(n.get("source_file"), main_root) not in touched]
        keep_ids = {n["id"] for n in kept}
        links = g.get("links", g.get("edges", []))
        edges = []
        for e in links:
            s = e.get("source"); t = e.get("target")
            s = s.get("id") if isinstance(s, dict) else s
            t = t.get("id") if isinstance(t, dict) else t
            if s in keep_ids and t in keep_ids:
                edges.append({"source": s, "target": t, "relation": e.get("relation"),
                              "confidence": e.get("confidence", "INFERRED"),
                              "confidence_score": e.get("confidence_score", 0.7),
                              "source_file": e.get("source_file"),
                              "source_location": e.get("source_location"),
                              "weight": e.get("weight", 1.0)})
        nodes = kept
        seen = set(keep_ids)
        got_files = set()
        for n in frag_nodes:
            if n["id"] not in seen:
                seen.add(n["id"]); nodes.append(n)
                got_files.add(n.get("source_file"))
        nodeset = {n["id"] for n in nodes}
        for e in frag_edges:
            if e.get("source") in nodeset and e.get("target") in nodeset \
                    and e["source"] != e["target"]:
                edges.append(e)
        hyper = g.get("hyperedges", [])
    else:  # full
        prev_count = 0
        seen = set()
        nodes = []
        for n in frag_nodes:
            if n["id"] not in seen:
                seen.add(n["id"]); nodes.append(n)
        nodeset = {n["id"] for n in nodes}
        edges = [e for e in frag_edges
                 if e.get("source") in nodeset and e.get("target") in nodeset
                 and e["source"] != e["target"]]
        hyper = frag_hyper

    # self-check: every nontrivial corpus file should yield >=1 node.
    def nontrivial(rel):
        try:
            return len(Path(main_root, rel).read_text(errors="ignore").strip()) >= 80
        except Exception:  # noqa: BLE001
            return False

    have_files = {rel_to_main(n.get("source_file"), main_root) for n in nodes}
    if args.mode == "full":
        expect = [c for c in corpus_rel if c.endswith(".md") and nontrivial(c)]
        missing = [c for c in expect if c not in have_files]
        if missing:
            print(f"ASSEMBLE_FAILED no nodes for {len(missing)} corpus files "
                  f"e.g. {missing[:8]}")
            raise SystemExit(2)

    ex = {"nodes": nodes, "edges": edges, "hyperedges": hyper,
          "input_tokens": 0, "output_tokens": 0}
    G = build_from_json(ex)

    if G.number_of_nodes() < args.min_nodes:
        print(f"ASSEMBLE_FAILED too few nodes: {G.number_of_nodes()}")
        raise SystemExit(2)
    if args.mode == "incremental" and prev_count and \
            G.number_of_nodes() < prev_count * 0.8 and not (args.deleted_file):
        print(f"ASSEMBLE_FAILED node count regressed {prev_count} -> {G.number_of_nodes()}")
        raise SystemExit(2)

    communities = cluster(G)
    cohesion = score_all(G, communities)
    gods = god_nodes(G)
    surprises = surprising_connections(G, communities)
    labels = {}
    for cid, mem in communities.items():
        labels[cid] = (G.nodes[mem[0]].get("label", f"Community {cid}")[:44]
                       if len(mem) == 1 else f"Community {cid}")

    # manifest keyed by main-relative path (portable across checkouts)
    man = {}
    for abs, rel in zip(corpus_abs, corpus_rel):
        if os.path.exists(abs):
            man[rel] = {"mtime": os.path.getmtime(abs),
                        "hash": hashlib.md5(open(abs, "rb").read()).hexdigest()}
    detection = {"total_files": len(man), "total_words": 0, "needs_graph": True,
                 "warning": None,
                 "files": {"document": corpus_rel, "code": [], "paper": [],
                           "image": [], "video": []}}
    questions = suggest_questions(G, communities, labels)
    report = generate(G, communities, cohesion, labels, gods, surprises, detection,
                      {"input": 0, "output": 0},
                      "goatos docs (origin/main .md + schema.html)",
                      suggested_questions=questions)

    os.makedirs(out, exist_ok=True)
    Path(f"{out}/GRAPH_REPORT.md").write_text(report)
    to_json(G, communities, f"{out}/graph.json")
    try:
        to_html(G, communities, f"{out}/graph.html", community_labels=labels)
    except Exception:  # noqa: BLE001
        pass
    Path(f"{out}/manifest.json").write_text(json.dumps(man, indent=2))
    Path(f"{out}/.docs_graph_basis").write_text(args.basis.strip() + "\n")

    print(f"ASSEMBLE_OK mode={args.mode} nodes={G.number_of_nodes()} "
          f"edges={G.number_of_edges()} communities={len(communities)} "
          f"files={len(man)} basis={args.basis[:12]}")


if __name__ == "__main__":
    main()
