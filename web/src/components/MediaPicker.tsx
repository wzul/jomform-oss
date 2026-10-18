"use client";

// Issue #391: WordPress-style media picker for the form logo field. Lets the
// merchant choose an image already in the media library, upload a new one
// (via the existing uploadImage endpoint), or paste an external URL as a
// power-user fallback. Reuses the media library (/api/rest/media) so the logo
// is a media item, consistent with #180/#382.

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "./i18n";
import { workspaceQuery, workspaceUrl } from "../lib/workspace";
import { apiErr } from "../lib/apiErr";

type MediaItem = {
  id: string;
  key: string;
  url: string;
  mime: string;
  size: number;
  source: string;
  sizes?: Record<string, string>;
  preview?: string;
  created_at: string;
};

function isImage(mime: string): boolean {
  return mime.startsWith("image/");
}

function nameFromKey(key: string): string {
  const parts = key.split("/");
  return parts[parts.length - 1] || key;
}

// Logo is an image, so only image types are offered for upload.
const ACCEPTED_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif", "image/svg+xml"];
const MAX_UPLOAD = 20 * 1024 * 1024;

export default function MediaPicker({
  open,
  onClose,
  onSelect,
  current,
}: {
  open: boolean;
  onClose: () => void;
  onSelect: (url: string) => void;
  current?: string;
}) {
  const { t } = useI18n();
  const [items, setItems] = useState<MediaItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadMsg, setUploadMsg] = useState("");
  const [showUrl, setShowUrl] = useState(false);
  const [urlValue, setUrlValue] = useState("");
  // Issue #501: the pasted-URL path is the one place a merchant can hand-type
  // an image URL; reject a non-https scheme here with the same message the
  // server would return, so the value never reaches a write path.
  const [urlErr, setUrlErr] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await fetch(workspaceUrl("/api/rest/media?limit=100"));
      if (r.ok) {
        const d = await r.json();
        setItems(Array.isArray(d.items) ? d.items.filter((m: MediaItem) => isImage(m.mime)) : []);
      } else {
        setItems([]);
      }
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) {
      setShowUrl(false);
      setUrlValue("");
      setUrlErr("");
      setUploadMsg("");
      load();
    }
  }, [open, load]);

  async function uploadFile(file: File) {
    if (!file) return;
    if (file.size > MAX_UPLOAD) {
      setUploadMsg(t("media.uploadTooLarge"));
      return;
    }
    setUploading(true);
    setUploadMsg("");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/image", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setUploadMsg(apiErr(d, t("media.uploadFail"), t));
        return;
      }
      // The same GIF the merchant picked is stored as one still image (first
      // frame), so this picker must answer like /media does — a bare
      // "Uploaded ✓" here is the same silence the hint above removes.
      setUploadMsg(t(file.type === "image/gif" ? "media.uploadedGifStill" : "media.uploaded"));
      load();
    } catch {
      setUploadMsg(t("media.uploadFail"));
    } finally {
      setUploading(false);
    }
  }

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl overflow-hidden rounded-xl bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-4 py-3">
          <h3 className="text-sm font-semibold">{t("mediaPicker.title")}</h3>
          <button
            onClick={onClose}
            className="text-[var(--jf-muted)] hover:text-slate-600"
            aria-label={t("mediaPicker.close")}
          >
            ✕
          </button>
        </div>
        <div className="p-4">
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={uploading}
              className="jf-btn-primary px-3 py-1.5 text-xs disabled:opacity-50"
            >
              {uploading ? t("media.uploading") : `+ ${t("mediaPicker.uploadNew")}`}
            </button>
            <button
              type="button"
              onClick={() => setShowUrl((v) => !v)}
              className="jf-btn-ghost px-3 py-1.5 text-xs"
            >
              {t("mediaPicker.pasteUrl")}
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept={ACCEPTED_TYPES.join(",")}
              className="hidden"
              onChange={(e) => {
                if (e.target.files?.[0]) uploadFile(e.target.files[0]);
                e.target.value = "";
              }}
            />
          </div>
          {/* t_ffc7f502: this picker uploads through the same optimiser, so the
              same rules apply here: opaque images become JPEG, an image with a
              transparent background stays PNG, an animated GIF keeps one frame. */}
          <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("media.imageStorage")}</p>
          {uploadMsg && <p className="mt-2 text-xs text-[#4f46e5]">{uploadMsg}</p>}

          {showUrl && (
            <div className="mt-3">
              <input
                value={urlValue}
                onChange={(e) => {
                  setUrlValue(e.target.value);
                  setUrlErr("");
                }}
                placeholder="https://cdn.example/logo.png"
                className="jf-input w-full text-xs"
              />
              {urlErr && <p className="mt-1 text-xs font-medium text-red-600">{urlErr}</p>}
              <button
                type="button"
                onClick={() => {
                  const v = urlValue.trim();
                  if (!v) return;
                  if (!v.startsWith("https://")) {
                    setUrlErr(t("url.notHttps"));
                    return;
                  }
                  setUrlErr("");
                  onSelect(v);
                }}
                className="jf-btn-primary mt-2 px-3 py-1.5 text-xs"
              >
                {t("mediaPicker.useUrl")}
              </button>
            </div>
          )}

          <div className="mt-4 max-h-80 overflow-y-auto">
            {loading ? (
              <p className="text-center text-sm text-[var(--jf-muted)]">…</p>
            ) : items.length === 0 ? (
              <p className="text-center text-sm text-[var(--jf-muted)]">{t("media.empty")}</p>
            ) : (
              <div className="grid grid-cols-3 gap-3 sm:grid-cols-4">
                {items.map((m) => (
                  <button
                    key={m.id}
                    type="button"
                    onClick={() => onSelect(m.url)}
                    className={`group overflow-hidden rounded-lg border text-left ${
                      current === m.url
                        ? "border-[#7170ff] ring-2 ring-[#7170ff]/40"
                        : "border-[var(--jf-border)] hover:border-[#7170ff]"
                    }`}
                  >
                    <div className="flex h-20 items-center justify-center overflow-hidden bg-slate-100">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={m.preview || m.sizes?.thumbnail || m.url}
                        alt={nameFromKey(m.key)}
                        className="h-full w-full object-cover"
                        loading="lazy"
                      />
                    </div>
                    <p className="truncate px-2 py-1 text-[11px] text-[var(--jf-muted)]">
                      {nameFromKey(m.key)}
                    </p>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
