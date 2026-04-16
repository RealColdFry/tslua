import { useRef, useCallback } from "react";
import MonacoEditor, { type OnMount } from "@monaco-editor/react";
import type { editor } from "monaco-editor";

interface EditorProps {
  value: string;
  language: string;
  readOnly?: boolean;
  path?: string;
  onChange?: (value: string) => void;
  onEditorMount?: (editor: editor.IStandaloneCodeEditor) => void;
  theme?: "dark" | "light";
}

export function Editor({
  value,
  language,
  readOnly,
  path,
  onChange,
  onEditorMount,
  theme = "dark",
}: EditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor>(null);

  const handleMount: OnMount = useCallback(
    (editor) => {
      editorRef.current = editor;
      onEditorMount?.(editor);
    },
    [onEditorMount],
  );

  const handleChange = useCallback(
    (val: string | undefined) => {
      if (onChange && val !== undefined) onChange(val);
    },
    [onChange],
  );

  return (
    <div className="pg-editor">
      <MonacoEditor
        height="100%"
        language={language}
        {...(path !== undefined && { path })}
        theme={theme === "light" ? "light" : "vs-dark"}
        value={value}
        {...(readOnly ? {} : { onChange: handleChange })}
        onMount={handleMount}
        options={{
          readOnly: readOnly ?? false,
          // Monaco defaults to "editable", which suppresses markers on
          // read-only models. Force "on" so setModelMarkers is visible
          // on the read-only Lua output pane.
          renderValidationDecorations: "on",
          fixedOverflowWidgets: true,
          minimap: { enabled: false },
          fontFamily: '"JetBrains Mono", "Fira Code", monospace',
          fontSize: 14,
          lineNumbers: "on",
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          wordWrap: "on",
          padding: { top: 12 },
        }}
      />
    </div>
  );
}
