import { redirect } from "next/navigation";

// Issue #475: the multi-workspace login picker moved from /space-picker to
// /workspace-picker as part of finishing the Space→Workspace rename (#431).
// This route stays as a permanent redirect so bookmarked/emailed links to the
// old path keep working instead of 404ing.
export default function LegacySpacePickerRedirect() {
  redirect("/workspace-picker");
}
