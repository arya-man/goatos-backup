import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";

/**
 * Config-owned health check: proves the real theme tokens, the real fonts and
 * the light/dark switch are all wired into Storybook. It intentionally imports
 * nothing from components/ or features/ so it stays green while those are
 * being edited.
 */
function ThemeSmoke() {
  const rows = [
    { id: "SF-048", shed: "Godel 1 - Part 1", due: 12, state: "Due" },
    { id: "SF-112", shed: "Godel 2 - Part 3", due: 4, state: "Done" },
    { id: "SF-203", shed: "Godel 3 - Part 1", due: 27, state: "Overdue" },
  ];
  return (
    <div style={{ padding: 24, background: "var(--bg)", color: "var(--fg)", minHeight: "100vh" }}>
      <h1 style={{ fontSize: 22, marginBottom: 4 }}>Storybook theme smoke</h1>
      <p className="muted" style={{ marginBottom: 20 }}>
        Public Sans, brand tokens and surfaces, straight from the app stylesheets.
      </p>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 20 }}>
        {["--bg", "--paper", "--paper-2", "--primary", "--primary-ink", "--info", "--warning", "--error"].map((token) => (
          <div
            key={token}
            style={{
              width: 116,
              borderRadius: "var(--r-md)",
              border: "1px solid var(--line)",
              overflow: "hidden",
              background: "var(--paper)",
            }}
          >
            <div style={{ height: 40, background: `var(${token})` }} />
            <div style={{ padding: "6px 8px", fontSize: 11, color: "var(--fg-muted)" }}>{token}</div>
          </div>
        ))}
      </div>

      <div
        style={{
          background: "var(--paper)",
          border: "1px solid var(--line)",
          borderRadius: "var(--r-lg)",
          boxShadow: "var(--shadow-card)",
          padding: 16,
          maxWidth: 680,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 12 }}>
          <strong>Vaccination queue</strong>
          <button
            type="button"
            style={{
              border: 0,
              borderRadius: "var(--r-pill)",
              padding: "8px 14px",
              background: "var(--primary)",
              color: "var(--on-brand)",
              fontWeight: 600,
              boxShadow: "var(--shadow-primary)",
            }}
          >
            Start round
          </button>
        </div>
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
          <thead>
            <tr style={{ color: "var(--fg-muted)", textAlign: "left" }}>
              <th style={{ padding: "6px 4px", borderBottom: "1px solid var(--line)" }}>Tag</th>
              <th style={{ padding: "6px 4px", borderBottom: "1px solid var(--line)" }}>Partition</th>
              <th style={{ padding: "6px 4px", borderBottom: "1px solid var(--line)" }}>Due</th>
              <th style={{ padding: "6px 4px", borderBottom: "1px solid var(--line)" }}>State</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id}>
                <td style={{ padding: "8px 4px", borderBottom: "1px solid var(--line2)" }}>{row.id}</td>
                <td style={{ padding: "8px 4px", borderBottom: "1px solid var(--line2)" }}>{row.shed}</td>
                <td style={{ padding: "8px 4px", borderBottom: "1px solid var(--line2)" }}>{row.due}</td>
                <td style={{ padding: "8px 4px", borderBottom: "1px solid var(--line2)" }}>
                  <span
                    style={{
                      borderRadius: "var(--r-pill)",
                      padding: "2px 10px",
                      fontSize: 11,
                      fontWeight: 600,
                      background:
                        row.state === "Overdue" ? "var(--dangerx)" : row.state === "Done" ? "var(--okx)" : "var(--warnx)",
                      color:
                        row.state === "Overdue" ? "var(--error-ink)" : row.state === "Done" ? "var(--success-ink)" : "var(--warning-ink)",
                    }}
                  >
                    {row.state}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

const meta = {
  title: "Smoke/Theme",
  component: ThemeSmoke,
  parameters: { layout: "fullscreen", dualTheme: { height: 760 } },
} satisfies Meta<typeof ThemeSmoke>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Mobile: Story = { globals: { viewport: { value: "mobile", isRotated: false } } };
