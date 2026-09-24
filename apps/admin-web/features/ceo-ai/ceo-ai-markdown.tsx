"use client";

import { Children, isValidElement, useEffect, useRef, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { Check, Copy } from "lucide-react";

// Rich rendering for coding-agent answers: GFM (tables, lists, links),
// highlighted code blocks with copy, and opt-in sandboxed HTML previews.

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        // Insecure-context webviews have no async clipboard: textarea fallback.
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        const ok = document.execCommand("copy");
        ta.remove();
        if (!ok) return;
      }
    } catch {
      return;
    }
    setDone(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setDone(false), 1500);
  };
  return (
    <button
      type="button"
      className="mzai-copy-ic"
      aria-label={done ? "Copied" : label}
      title={done ? "Copied" : label}
      onClick={() => void copy()}
    >
      {done ? <Check size={14} /> : <Copy size={14} />}
    </button>
  );
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const child = Children.toArray(children)[0];
  const className =
    isValidElement<{ className?: string }>(child) ? (child.props.className ?? "") : "";
  const lang = /language-([\w-]+)/.exec(className)?.[1] ?? "";
  const code = textOf(children).replace(/\n$/, "");
  const isHtml = lang === "html" || lang === "svg";
  const [preview, setPreview] = useState(false);

  return (
    <div className="mzai-code">
      <div className="mzai-code-head">
        <span>{lang || "text"}</span>
        <span className="mzai-code-actions">
          {isHtml ? (
            <button type="button" className="mzai-copy" onClick={() => setPreview((p) => !p)}>
              {preview ? "Code" : "Preview"}
            </button>
          ) : null}
          <CopyButton text={code} label="Copy code" />
        </span>
      </div>
      {isHtml && preview ? (
        <iframe className="mzai-html" sandbox="" srcDoc={code} title="HTML preview" />
      ) : (
        <pre tabIndex={0}>{children}</pre>
      )}
    </div>
  );
}

export function CeoAiMarkdown({ text }: { text: string }) {
  return (
    <div className="mzai-md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: true, ignoreMissing: true }]]}
        components={{
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer noopener">
              {children}
            </a>
          ),
          table: ({ children }) => (
            <div className="mzai-table" tabIndex={0}>
              <table>{children}</table>
            </div>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
