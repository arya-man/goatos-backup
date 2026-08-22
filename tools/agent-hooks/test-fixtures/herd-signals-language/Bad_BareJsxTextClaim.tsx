// Bad_BareJsxTextClaim.tsx — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 5, bypass 1: a bare JSX text
// child (a column header, a status chip) carries no quote characters at
// all, so quotedClaim (which requires quotes) used to let these pass with
// zero findings. jsxTextClaim closes this. This file must never be wired
// into a real build target; it exists only for the guard's --self-test.

export function BareJsxClaims() {
  return (
    <table>
      <thead>
        <tr>
          <th>Eating</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td><Tag tone="ok">Ruminating</Tag></td>
        </tr>
      </tbody>
    </table>
  );
}
