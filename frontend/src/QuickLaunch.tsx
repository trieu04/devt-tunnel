import { Service } from "./api";
import { QuickPalette } from "./quick/QuickPalette";

export function QuickLaunch() {
  return (
    <QuickPalette
      onClose={() => Service.HideQuickLaunch()}
      onNavigate={(screen, param = "") => Service.OpenIn(screen, param)}
    />
  );
}
