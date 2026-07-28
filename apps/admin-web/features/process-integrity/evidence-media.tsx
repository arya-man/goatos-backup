import { ExternalLink } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
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
      <span className="evidence-media">
        {media.map((item, index) => (
          <a key={item.proof_id || `${item.download_url}-${index}`} href={item.download_url} target="_blank" rel="noreferrer" className={`tag t-${tone}`}>
            {mediaLabel(pageContract, item)}
            <ExternalLink className="ic" aria-hidden="true" />
          </a>
        ))}
      </span>
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

  return <span className="muted">-</span>;
}
