import { useEffect, useRef } from "preact/hooks";
import { Compartment, EditorState } from "@codemirror/state";
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { syntaxHighlighting, HighlightStyle } from "@codemirror/language";
import { yaml } from "@codemirror/lang-yaml";
import { tags } from "@lezer/highlight";

const editorHighlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.operatorKeyword], color: "var(--syntax-keyword)" },
  {
    tag: [tags.string, tags.special(tags.string)],
    color: "var(--syntax-string)",
  },
  { tag: [tags.number, tags.bool, tags.atom], color: "var(--syntax-number)" },
  { tag: [tags.comment, tags.meta], color: "var(--syntax-comment)" },
  {
    tag: [tags.propertyName, tags.attributeName],
    color: "var(--syntax-property)",
  },
]);

export function CodeEditor({
  initial,
  readOnly = false,
  onChange,
}: {
  initial: string;
  readOnly?: boolean;
  onChange: (value: string) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const change = useRef(onChange);
  change.current = onChange;
  const editor = useRef<EditorView>();
  const editable = useRef(new Compartment());
  useEffect(() => {
    const view = new EditorView({
      parent: parent.current!,
      state: EditorState.create({
        doc: initial,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          history(),
          yaml(),
          syntaxHighlighting(editorHighlight),
          editable.current.of([
            EditorState.readOnly.of(readOnly),
            EditorView.editable.of(!readOnly),
          ]),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          EditorView.contentAttributes.of({ "aria-label": "File contents" }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) change.current(update.state.doc.toString());
          }),
          EditorView.theme({
            "&": {
              height: "100%",
              background: "var(--surface)",
              color: "var(--text-strong)",
            },
            ".cm-cursor": { borderLeftColor: "var(--text-strong)" },
            ".cm-activeLine": { background: "var(--editor-active-line)" },
            ".cm-activeLineGutter": { background: "var(--editor-active-line)" },
            ".cm-scroller": {
              overflow: "auto",
              fontFamily: "var(--mono)",
              fontSize: "13px",
              lineHeight: "1.65",
            },
            ".cm-gutters": {
              background: "var(--surface-soft)",
              border: "none",
              color: "var(--muted)",
            },
            ".cm-content": { padding: "14px 0" },
            ".cm-line": { paddingLeft: "12px" },
          }),
        ],
      }),
    });
    editor.current = view;
    return () => view.destroy();
  }, [initial]);
  useEffect(() => {
    editor.current?.dispatch({
      effects: editable.current.reconfigure([
        EditorState.readOnly.of(readOnly),
        EditorView.editable.of(!readOnly),
      ]),
    });
  }, [readOnly]);
  return <div class="code-editor" ref={parent} />;
}
