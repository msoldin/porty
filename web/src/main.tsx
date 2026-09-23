import { render } from "preact";
import { App } from "./App";
import "./styles.css";
import { applySavedTheme } from "./theme";
applySavedTheme();
render(<App />, document.getElementById("app")!);
