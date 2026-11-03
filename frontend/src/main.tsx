import React from "react";
import ReactDOM from "react-dom/client";
import { MainWindow } from "./MainWindow";
import { QuickLaunch } from "./QuickLaunch";
import "./styles.css";

const isQuick = new URLSearchParams(window.location.search).get("view") === "quick";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>{isQuick ? <QuickLaunch /> : <MainWindow />}</React.StrictMode>,
);
