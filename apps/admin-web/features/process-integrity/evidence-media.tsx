import Box from "@mui/material/Box";
import Link from "@mui/material/Link";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { Tag, TONE_COLOR } from "@/components/ui-primitives";
import type { ProcessIntegrityEvidence } from "@/lib/api/server";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { Tone } from "./process-integrity";

type EvidenceMediaItem = {
  proof_id: string;
  download_url: string;
  mime_type?: string;
  duration_ms?: number;
};

export type EvidenceWithMedia = ProcessIntegrityEvidence & {
  media?: EvidenceMediaItem[];
  media_resolution_error?: string;
};

function mediaLabel(pageContract: AdminUiPageContract, media: EvidenceMediaItem): string {
  if (media.mime_type?.startsWith("video/")) return copy(pageContract, "label.video_proof");
  if (media.mime_type?.startsWith("image/")) return copy(pageContract, "label.image_proof");
  return copy(pageContract, "label.open_proof");
}

export function EvidenceMedia({ evidence, pageContract, tone = "ok" }: { evidence: EvidenceWithMedia; pageContract: AdminUiPageContract; tone?: Tone }) {
  if (evidence.latest_rejection_reason) {
    return <Tag tone="dng" title={evidence.latest_rejection_reason}>{copy(pageContract, "label.rejected")}</Tag>;
  }

  const media = evidence.media ?? [];
  if (media.length > 0) {
    return (
      <Box component="span" data-testid="evidence-media" sx={{ display: "inline-flex", flexWrap: "wrap", gap: 0.5 }}>
        {media.map((item, index) => (
          <Link key={item.proof_id || `${item.download_url}-${index}`} href={item.download_url} target="_blank" rel="noreferrer" underline="none">
            <Label variant="soft" color={TONE_COLOR[tone] ?? "default"} endIcon={<Iconify icon="eva:external-link-fill" aria-hidden="true" />} sx={{ cursor: "pointer" }}>
              {mediaLabel(pageContract, item)}
            </Label>
          </Link>
        ))}
      </Box>
    );
  }

  if (evidence.media_resolution_error) {
    return (
      <Tag tone="warn" title={evidence.media_resolution_error}>
        {copy(pageContract, "label.proof_media_unavailable")}
      </Tag>
    );
  }

  if (evidence.evidence_count > 0) {
    return (
      <Tag tone={tone} title={evidence.audit_ref ?? undefined}>
        {evidence.evidence_count} {copy(pageContract, evidence.evidence_count === 1 ? "label.proof_singular" : "label.proof_plural")}
      </Tag>
    );
  }

  return <Box component="span" sx={{ color: "text.disabled" }}>-</Box>;
}
