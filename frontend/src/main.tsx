import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./app/App";
import { QuickLaunch } from "./QuickLaunch";
import { TrayPopup } from "./TrayPopup";
import "./styles.css";

const view = new URLSearchParams(window.location.search).get("view");

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>{view === "quick" ? <QuickLaunch /> : view === "tray" ? <TrayPopup /> : <App />}</React.StrictMode>,
);
