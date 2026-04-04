import { useRef, useCallback } from "react";
import MonacoEditor, { type OnMount } from "@monaco-editor/react";
import type { editor } from "monaco-editor";

interface EditorProps {
  value: string;
  language: string;
  readOnly?: boolean;
  path?: string;
  onChange?: (value: string) => void;
  theme?: "dark" | "light";
}

export function Editor({ value, language, readOnly, path, onChange, theme = "dark" }: EditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor>(null);

  const handleMount: OnMount = useCallback((editor) => {
    editorRef.current = editor;
  }, []);

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
        path={path}
        theme={theme === "light" ? "light" : "vs-dark"}
        value={value}
        onChange={readOnly ? undefined : handleChange}
        onMount={handleMount}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontFamily: '"Iosevka", "JetBrains Mono", "Fira Code", monospace',
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
