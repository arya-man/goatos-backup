// useReducer state + actions for the SOP Builder. All updates are immutable.

import type { BuilderField, BuilderLink, BuilderNode, BuilderRule, BuilderState, BuilderTab, FieldType, SopMeta } from "./model";
import type { SopTemplate } from "./sop-templates";
import { FLOW_PATTERNS, NODE_PALETTE, slugify, titleCase } from "./templates";

export interface BuilderSeed {
  meta: SopMeta;
  fields: BuilderField[];
  rules: BuilderRule[];
  nodes: BuilderNode[];
  links: BuilderLink[];
  flowPattern: string;
  activeTemplate: string | null;
}

export function initialState(seed: BuilderSeed): BuilderState {
  return {
    meta: seed.meta,
    fields: seed.fields,
    rules: seed.rules,
    nodes: seed.nodes,
    links: seed.links,
    flowPattern: seed.flowPattern,
    activeTemplate: seed.activeTemplate,
    selectedFieldIndex: seed.fields.length ? 0 : null,
    draftField: null,
    selectedNodeId: seed.nodes[0]?.id ?? null,
    selectedLinkIndex: null,
    activeTab: "catalog",
    dirty: true,
  };
}

export function templateToSeed(key: string, template: SopTemplate, flowPattern: string): BuilderSeed {
  return {
    meta: template.meta,
    fields: template.fields,
    rules: template.rules,
    nodes: template.nodes,
    links: template.links,
    flowPattern,
    activeTemplate: key,
  };
}

let counter = 0;
function uid(prefix: string): string {
  counter += 1;
  return `${prefix}_${counter}`;
}

function uniqueKey(base: string, existing: string[]): string {
  const root = slugify(base, "field");
  if (!existing.includes(root)) return root;
  let i = 2;
  while (existing.includes(`${root}_${i}`)) i += 1;
  return `${root}_${i}`;
}

export function buildFieldDraft(type: FieldType, fields: BuilderField[]): BuilderField {
  const key = uniqueKey(type, fields.map((f) => f.key));
  return {
    key,
    label: titleCase(key),
    type,
    required: false,
    description: "",
    placeholder: "",
    defaultValue: "",
    options: "",
    custom: true,
  };
}

export type Action =
  | { kind: "setTab"; tab: BuilderTab }
  | { kind: "loadSeed"; seed: BuilderSeed }
  | { kind: "updateMeta"; patch: Partial<SopMeta> }
  | { kind: "startDraft"; fieldType: FieldType }
  | { kind: "editDraft"; patch: Partial<BuilderField> }
  | { kind: "commitDraft" }
  | { kind: "discardDraft" }
  | { kind: "selectField"; index: number }
  | { kind: "editField"; index: number; patch: Partial<BuilderField> }
  | { kind: "toggleRequired"; index: number }
  | { kind: "removeField"; index: number }
  | { kind: "reorderField"; from: number; to: number }
  | { kind: "addRule"; rule: BuilderRule }
  | { kind: "updateRule"; id: string; patch: Partial<BuilderRule> }
  | { kind: "removeRule"; id: string }
  | { kind: "setFlowPattern"; pattern: string }
  | { kind: "addNode"; roleKey: string; label: string; after: string }
  | { kind: "selectNode"; id: string }
  | { kind: "updateNode"; id: string; patch: Partial<BuilderNode> }
  | { kind: "moveNode"; id: string; x: number; y: number }
  | { kind: "deleteNode"; id: string }
  | { kind: "selectLink"; index: number | null }
  | { kind: "saveLink"; from: string; to: string; label: string; about: string; replaceIndex: number | null }
  | { kind: "removeLink"; index: number }
  | { kind: "markSaved" };

function dirty(state: BuilderState, patch: Partial<BuilderState>): BuilderState {
  return { ...state, ...patch, dirty: true };
}

function addConnector(links: BuilderLink[], from: string, to: string, label: string, about: string): { links: BuilderLink[]; index: number } {
  if (!from || !to || from === to) return { links, index: -1 };
  const idx = links.findIndex((l) => l[0] === from && l[1] === to);
  const clean: BuilderLink = [from, to, label || "next", about];
  if (idx >= 0) {
    const next = links.map((l, i) => (i === idx ? clean : l));
    return { links: next, index: idx };
  }
  return { links: [...links, clean], index: links.length };
}

export function reducer(state: BuilderState, action: Action): BuilderState {
  switch (action.kind) {
    case "setTab":
      return { ...state, activeTab: action.tab };

    case "loadSeed":
      return initialState(action.seed);

    case "updateMeta":
      return dirty(state, { meta: { ...state.meta, ...action.patch } });

    case "startDraft":
      return { ...state, draftField: buildFieldDraft(action.fieldType, state.fields), selectedFieldIndex: null };

    case "editDraft":
      return state.draftField ? { ...state, draftField: { ...state.draftField, ...action.patch }, dirty: true } : state;

    case "commitDraft": {
      if (!state.draftField) return state;
      const key = uniqueKey(state.draftField.key, state.fields.map((f) => f.key));
      const field = { ...state.draftField, key };
      const fields = [...state.fields, field];
      return dirty(state, { fields, draftField: null, selectedFieldIndex: fields.length - 1 });
    }

    case "discardDraft":
      return { ...state, draftField: null };

    case "selectField":
      return { ...state, selectedFieldIndex: action.index, draftField: null };

    case "editField": {
      const fields = state.fields.map((f, i) => (i === action.index ? { ...f, ...action.patch } : f));
      return dirty(state, { fields });
    }

    case "toggleRequired": {
      const fields = state.fields.map((f, i) => (i === action.index ? { ...f, required: !f.required } : f));
      return dirty(state, { fields, selectedFieldIndex: action.index });
    }

    case "removeField": {
      const fields = state.fields.filter((_, i) => i !== action.index);
      const selectedFieldIndex = fields.length ? Math.min(action.index, fields.length - 1) : null;
      return dirty(state, { fields, selectedFieldIndex });
    }

    case "reorderField": {
      const fields = [...state.fields];
      const [moved] = fields.splice(action.from, 1);
      fields.splice(action.to, 0, moved);
      return dirty(state, { fields, selectedFieldIndex: action.to });
    }

    case "addRule":
      return dirty(state, { rules: [...state.rules, action.rule] });

    case "updateRule":
      return dirty(state, { rules: state.rules.map((r) => (r.id === action.id ? { ...r, ...action.patch } : r)) });

    case "removeRule":
      return dirty(state, { rules: state.rules.filter((r) => r.id !== action.id) });

    case "setFlowPattern": {
      const flow = FLOW_PATTERNS[action.pattern] ?? FLOW_PATTERNS.simple;
      return dirty(state, {
        flowPattern: action.pattern,
        nodes: flow.nodes.map((n) => ({ ...n })),
        links: flow.links.map((l) => [...l] as BuilderLink),
        selectedNodeId: flow.nodes[0]?.id ?? null,
        selectedLinkIndex: null,
      });
    }

    case "addNode": {
      const spec = NODE_PALETTE.find((p) => p.key === action.roleKey);
      const base = state.nodes.find((n) => n.id === action.after) ?? state.nodes[0];
      const label = action.label.trim() || spec?.label || "Custom step";
      const id = uid(slugify(label, "node"));
      const node: BuilderNode = {
        id,
        label,
        type: spec?.type ?? "operator_execution",
        role: spec?.role ?? "operator",
        x: base ? Math.min(base.x + 210, 590) : 18,
        y: base ? Math.min(base.y + 96, 360) : 40,
      };
      const { links } = addConnector(state.links, action.after, id, "next", "");
      return dirty(state, { nodes: [...state.nodes, node], links, selectedNodeId: id });
    }

    case "selectNode":
      return { ...state, selectedNodeId: action.id };

    case "updateNode":
      return dirty(state, { nodes: state.nodes.map((n) => (n.id === action.id ? { ...n, ...action.patch } : n)) });

    case "moveNode":
      return { ...state, nodes: state.nodes.map((n) => (n.id === action.id ? { ...n, x: action.x, y: action.y } : n)), dirty: true };

    case "deleteNode": {
      if (action.id === "start" || state.nodes.length <= 1) return state;
      const nodes = state.nodes.filter((n) => n.id !== action.id);
      const links = state.links.filter((l) => l[0] !== action.id && l[1] !== action.id);
      return dirty(state, { nodes, links, selectedNodeId: nodes[0]?.id ?? null, selectedLinkIndex: null });
    }

    case "selectLink":
      return { ...state, selectedLinkIndex: action.index };

    case "saveLink": {
      let links = state.links;
      if (action.replaceIndex !== null && links[action.replaceIndex]) {
        const cur = links[action.replaceIndex];
        if (cur[0] !== action.from || cur[1] !== action.to) {
          links = links.filter((_, i) => i !== action.replaceIndex);
        }
      }
      const result = addConnector(links, action.from, action.to, action.label, action.about);
      return dirty(state, { links: result.links, selectedLinkIndex: result.index >= 0 ? result.index : state.selectedLinkIndex });
    }

    case "removeLink": {
      const links = state.links.filter((_, i) => i !== action.index);
      return dirty(state, { links, selectedLinkIndex: null });
    }

    case "markSaved":
      return { ...state, dirty: false };

    default:
      return state;
  }
}
