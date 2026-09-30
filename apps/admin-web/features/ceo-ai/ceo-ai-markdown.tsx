"use client";
import Table from "@mui/material/Table";
import Box from "@mui/material/Box";
import ButtonBase from "@mui/material/ButtonBase";
import {
  codeActionsSx,
  codeBlockSx,
  codeHeadSx,
  codePreSx,
  codePreviewToggleSx,
  htmlPreviewSx,
  markdownSx,
  markdownTableSx,
} from "./ceo-ai-sx";

import { Children, isValidElement, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { CopyButton } from "./ceo-ai-copy-button";

// Rich rendering for coding-agent answers: GFM (tables, lists, links),
// highlighted code blocks with copy, and opt-in sandboxed HTML previews.
// Loaded lazily by the panel (react-markdown + highlight.js are ~100 KB gzip) so
// they stay out of every page's first-load JS.

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
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
    <Box sx={codeBlockSx}>
      <Box sx={codeHeadSx}>
        <span>{lang || "text"}</span>
        <Box component="span" sx={codeActionsSx}>
          {isHtml ? (
            <ButtonBase sx={codePreviewToggleSx} onClick={() => setPreview((p) => !p)}>
              {preview ? "Code" : "Preview"}
            </ButtonBase>
          ) : null}
          <CopyButton text={code} label="Copy code" inCode />
        </Box>
      </Box>
      {isHtml && preview ? (
        <Box component="iframe" sx={htmlPreviewSx} sandbox="" srcDoc={code} title="HTML preview" />
      ) : (
        <Box component="pre" sx={codePreSx} tabIndex={0}>
          {children}
        </Box>
      )}
    </Box>
  );
}

export function CeoAiMarkdown({ text }: { text: string }) {
  return (
    <Box sx={markdownSx}>
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
            <Box sx={markdownTableSx} tabIndex={0}>
              <Table>{children}</Table>
            </Box>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </Box>
  );
}
