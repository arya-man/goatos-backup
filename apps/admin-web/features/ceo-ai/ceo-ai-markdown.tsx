"use client";

import { Children, isValidElement, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import Box from "@mui/material/Box";
import Link from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import TableContainer from "@mui/material/TableContainer";
import { MarkdownRoot } from "@/components/minimal/markdown/styles";
import { markdownClasses } from "@/components/minimal/markdown/classes";
import { CopyButton } from "./ceo-ai-copy-button";

// Rich answers on the template Markdown anatomy (components/minimal/markdown: MarkdownRoot +
// markdownClasses, verbatim): GFM tables, lists, links, highlighted code blocks with Copy and an
// opt-in sandboxed HTML preview. Not the template's <Markdown> component itself: that one runs
// rehype-raw (raw HTML from a model answer would render unsanitised) and turndown. Declared
// override: CHAT_SCALE sets the template's page type scale (h1 64px, body1 paragraphs) to the chat
// bubble's body2 scale. Loaded lazily by the panel (react-markdown + highlight.js are ~100 KB gzip).

const CHAT_SCALE = {
  typography: "body2",
  overflowWrap: "anywhere",
  "& h1, & h2, & h3": { typography: "subtitle1", mt: 2, mb: 1 },
  "& h4, & h5, & h6": { typography: "subtitle2", mt: 1.5, mb: 0.5 },
  "& p": { typography: "body2", mb: 1 },
  "& ol": { listStyleType: "decimal" },
  "& > :first-child": { mt: 0 },
  "& > :last-child": { mb: 0 },
} as const;

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const child = Children.toArray(children)[0];
  const className = isValidElement<{ className?: string }>(child) ? (child.props.className ?? "") : "";
  const lang = /language-([\w-]+)/.exec(className)?.[1] ?? "";
  const code = textOf(children).replace(/\n$/, "");
  const isHtml = lang === "html" || lang === "svg";
  const [preview, setPreview] = useState(false);

  return (
    <div className={markdownClasses.content.codeBlock}>
      <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 0.5 }}>
        <Typography variant="caption" sx={{ flexGrow: 1, color: "text.secondary" }}>
          {lang || "text"}
        </Typography>
        {isHtml ? (
          <Button size="small" color="inherit" onClick={() => setPreview((p) => !p)}>
            {preview ? "Code" : "Preview"}
          </Button>
        ) : null}
        <CopyButton text={code} label="Copy code" />
      </Stack>
      {isHtml && preview ? (
        <Box component="iframe" sandbox="" srcDoc={code} title="HTML preview" sx={{ width: 1, height: "calc(40 * var(--spacing))", border: 0, bgcolor: "background.paper" }} />
      ) : (
        <pre tabIndex={0}>{children}</pre>
      )}
    </div>
  );
}

export function CeoAiMarkdown({ text }: { text: string }) {
  return (
    <MarkdownRoot className={markdownClasses.root} sx={CHAT_SCALE}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: true, ignoreMissing: true }]]}
        components={{
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
          code: ({ className = "", children }) => (
            <code className={/language-\w+/.test(className) ? className : markdownClasses.content.codeInline}>{children}</code>
          ),
          a: ({ href, children }) => (
            <Link href={href} target="_blank" rel="noreferrer noopener" className={markdownClasses.content.link}>
              {children}
            </Link>
          ),
          table: ({ children }) => (
            <TableContainer tabIndex={0}>
              <Table>{children}</Table>
            </TableContainer>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </MarkdownRoot>
  );
}
