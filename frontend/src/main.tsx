import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./app/App";
import { QuickLaunch } from "./QuickLaunch";
import { TrayPopup } from "./TrayPopup";
import { openURL } from "./api";
import "./styles.css";

// Never let the webview navigate away from the app: open external links in the system browser.
document.addEventListener("click", (event) => {
  if (event.defaultPrevented) return;
  const anchor = (event.target as Element | null)?.closest?.("a[href]");
  if (!anchor) return;
  const href = anchor.getAttribute("href") || "";
  if (/^https?:\/\//i.test(href)) {
    event.preventDefault();
    void openURL(href);
  }
});

const view = new URLSearchParams(window.location.search).get("view");

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>{view === "quick" ? <QuickLaunch /> : view === "tray" ? <TrayPopup /> : <App />}</React.StrictMode>,
);
