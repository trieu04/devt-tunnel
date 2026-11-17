import type { ToolDownloadOffer } from "../bindings/github.com/trieu04/devt-tunnel/internal/api";
import { cancelToolDownload, startToolDownload, useToolDownload } from "./downloads";
import { downloadErrorText, formatSize, progressFraction, progressText } from "./lib/binaries";

/**
 * The download row of a tool: the Download button, progress, verification and
 * failure with Retry. Renders nothing when there is no offer and no download.
 */
export function DownloadControl({
  tool,
  offer,
  primary = false,
  newer = false,
}: {
  tool: string;
  offer?: ToolDownloadOffer | null;
  /** Download is the primary action (empty state). */
  primary?: boolean;
  /** A downloaded binary is selected and the offer is newer. */
  newer?: boolean;
}) {
  const state = useToolDownload(tool);
  const version = state?.version || offer?.version || "";
  if (state?.state === "downloading") {
    const fraction = progressFraction(state.received, state.total);
    return (
      <div className="download-row" role="status">
        <progress className="download-bar" aria-label={`Downloading ${tool}`} max={1} value={fraction ?? undefined} />
        <span className="muted download-text">{progressText(state.received, state.total)}</span>
        <button onClick={() => void cancelToolDownload(tool)}>Cancel</button>
      </div>
    );
  }
  if (state?.state === "verifying") {
    return (
      <div className="download-row" role="status">
        <progress className="download-bar" aria-label="Verifying checksum" max={1} />
        <span className="muted download-text">Verifying checksum…</span>
      </div>
    );
  }
  if (state?.state === "failed") {
    return (
      <div className="download-row" role="alert">
        <span className="error-text download-text">{downloadErrorText(state.error)}</span>
        <button onClick={() => void startToolDownload(tool, version)}>Retry</button>
      </div>
    );
  }
  if (!offer) return null;
  return (
    <div className="download-row">
      {newer && <span className="muted download-text">{offer.version} is available</span>}
      <button className={primary ? "primary" : ""} onClick={() => void startToolDownload(tool, offer.version)}>
        ⤓ Download {tool} {offer.version} · {formatSize(offer.size)}
      </button>
      <span className="muted download-text">from GitHub (Cloudflare)</span>
    </div>
  );
}
