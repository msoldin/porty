import { render } from "preact";
import { App } from "./app/App";
import "./app/styles.css";
import "./app/tokens.css";
import "./app/shell.css";
import { applySavedTheme } from "./app/theme";
applySavedTheme();
render(<App />, document.getElementById("app")!);
