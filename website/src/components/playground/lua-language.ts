import type { languages } from "monaco-editor";

// Monarch tokenizer for Lua syntax highlighting
export const luaLanguage: languages.IMonarchLanguage = {
  defaultToken: "",
  tokenPostfix: ".lua",

  keywords: [
    "and",
    "break",
    "do",
    "else",
    "elseif",
    "end",
    "false",
    "for",
    "function",
    "goto",
    "if",
    "in",
    "local",
    "nil",
    "not",
    "or",
    "repeat",
    "return",
    "then",
    "true",
    "until",
    "while",
  ],

  brackets: [
    { open: "{", close: "}", token: "delimiter.bracket" },
    { open: "[", close: "]", token: "delimiter.array" },
    { open: "(", close: ")", token: "delimiter.parenthesis" },
  ],

  operators: [
    "+",
    "-",
    "*",
    "/",
    "%",
    "^",
    "#",
    "==",
    "~=",
    "<",
    ">",
    "<=",
    ">=",
    "=",
    ";",
    ":",
    ",",
    ".",
    "..",
    "...",
  ],

  tokenizer: {
    root: [
      // Comments
      [/--\[([=]*)\[/, "comment", "@longcomment.$1"],
      [/--.*$/, "comment"],

      // Strings
      [/\[([=]*)\[/, "string", "@longstring.$1"],
      [/"([^"\\]|\\.)*$/, "string.invalid"],
      [/'([^'\\]|\\.)*$/, "string.invalid"],
      [/"/, "string", "@string_double"],
      [/'/, "string", "@string_single"],

      // Numbers
      [/0[xX][0-9a-fA-F_]*/, "number.hex"],
      [/\d+(\.\d+)?([eE][+-]?\d+)?/, "number"],
      [/\.\d+([eE][+-]?\d+)?/, "number.float"],

      // Identifiers and keywords
      [
        /[a-zA-Z_]\w*/,
        {
          cases: {
            "@keywords": { token: "keyword.$0" },
            "@default": "identifier",
          },
        },
      ],

      // Whitespace
      [/\s+/, "white"],

      // Operators
      [/[{}()[\]]/, "@brackets"],
      [/[+\-*/%^#=<>~;:,.]/, "operator"],
    ],

    longcomment: [
      [/[^\]]+/, "comment"],
      [
        /\]([=]*)\]/,
        {
          cases: {
            "$1==$S2": { token: "comment", next: "@pop" },
            "@default": "comment",
          },
        },
      ],
      [/./, "comment"],
    ],

    longstring: [
      [/[^\]]+/, "string"],
      [
        /\]([=]*)\]/,
        {
          cases: {
            "$1==$S2": { token: "string", next: "@pop" },
            "@default": "string",
          },
        },
      ],
      [/./, "string"],
    ],

    string_double: [
      [/[^\\"]+/, "string"],
      [/\\./, "string.escape"],
      [/"/, "string", "@pop"],
    ],

    string_single: [
      [/[^\\']+/, "string"],
      [/\\./, "string.escape"],
      [/'/, "string", "@pop"],
    ],
  },
};
