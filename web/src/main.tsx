import { render } from "preact";
import "./app/styles.css";
import "./app/tokens.css";
import "./app/shell.css";
import { App } from "./app/App";
import { applySavedTheme } from "./app/theme";
applySavedTheme();
render(<App />, document.getElementById("app")!);
