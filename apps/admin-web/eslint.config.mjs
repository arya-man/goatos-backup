import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";

const config = [
  ...nextVitals,
  ...nextTypescript,
  // MUI Minimal v7.7.0 template code copied verbatim (licensed, see /Users/raviteja/mesha/mui-migration.md).
  // It follows the template's own lint profile; these rules are relaxed so the copy stays template-exact.
  {
    files: ["theme/**", "layouts/**"],
    rules: {
      "@typescript-eslint/no-empty-object-type": "off",
      "@typescript-eslint/no-unused-vars": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "import/no-anonymous-default-export": "off",
      "react-hooks/refs": "off",
      "react-hooks/set-state-in-effect": "off",
      "react-hooks/immutability": "off",
      "react-hooks/exhaustive-deps": "off",
    },
    linterOptions: { reportUnusedDisableDirectives: "off" },
  },
];

export default config;
