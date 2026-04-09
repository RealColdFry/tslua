
// Based on https://www.lua.org/manual/5.3/manual.html#6.2

/** @noSelfInFile */

/**
 * This library comprises the operations to manipulate coroutines, which come
 * inside the table coroutine.
 */
declare namespace coroutine {
    /**
     * Creates a new coroutine, with body f. f must be a function. Returns this
     * new coroutine, an object with type "thread".
     */
    function create(f: (...args: any[]) => any): LuaThread;

    /**
     * Starts or continues the execution of coroutine co. The first time you
     * resume a coroutine, it starts running its body. The values val1, ... are
     * passed as the arguments to the body function. If the coroutine has yielded,
     * resume restarts it; the values val1, ... are passed as the results from the
     * yield.
     *
     * If the coroutine runs without any errors, resume returns true plus any
     * values passed to yield (when the coroutine yields) or any values returned
     * by the body function (when the coroutine terminates). If there is any
     * error, resume returns false plus the error message.
     */
    function resume(
        co: LuaThread,
        ...val: any[]
    ): LuaMultiReturn<[true, ...any[]] | [false, string]>;

    /**
     * Returns the status of coroutine co, as a string: "running", if the
     * coroutine is running (that is, it called status); "suspended", if the
     * coroutine is suspended in a call to yield, or if it has not started running
     * yet; "normal" if the coroutine is active but not running (that is, it has
     * resumed another coroutine); and "dead" if the coroutine has finished its
     * body function, or if it has stopped with an error.
     */
    function status(co: LuaThread): 'running' | 'suspended' | 'normal' | 'dead';

    /**
     * Creates a new coroutine, with body f. f must be a function. Returns a
     * function that resumes the coroutine each time it is called. Any arguments
     * passed to the function behave as the extra arguments to resume. Returns the
     * same values returned by resume, except the first boolean. In case of error,
     * propagates the error.
     */
    function wrap(f: (...args: any[]) => any): (...args: any[]) => LuaMultiReturn<any[]>;

    /**
     * Suspends the execution of the calling coroutine. Any arguments to yield are
     * passed as extra results to resume.
     */
    function yield(...args: any[]): LuaMultiReturn<any[]>;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.10

/** @noSelfInFile */

/**
 * This library provides the functionality of the debug interface (§4.9) to Lua
 * programs. You should exert care when using this library. Several of its
 * functions violate basic assumptions about Lua code (e.g., that variables
 * local to a function cannot be accessed from outside; that userdata metatables
 * cannot be changed by Lua code; that Lua programs do not crash) and therefore
 * can compromise otherwise secure code. Moreover, some functions in this
 * library may be slow.
 *
 * All functions in this library are provided inside the debug table. All
 * functions that operate over a thread have an optional first argument which is
 * the thread to operate over. The default is always the current thread.
 */
declare namespace debug {
    /**
     * Enters an interactive mode with the user, running each string that the user
     * enters. Using simple commands and other debug facilities, the user can
     * inspect global and local variables, change their values, evaluate
     * expressions, and so on. A line containing only the word cont finishes this
     * function, so that the caller continues its execution.
     *
     * Note that commands for debug.debug are not lexically nested within any
     * function and so have no direct access to local variables.
     */
    function debug(): void;

    /**
     * Returns the current hook settings of the thread, as three values: the
     * current hook function, the current hook mask, and the current hook count
     * (as set by the debug.sethook function).
     */
    function gethook(
        thread?: LuaThread
    ): LuaMultiReturn<[undefined, 0] | [Function, number, string?]>;

    interface FunctionInfo<T extends Function = Function> {
        /**
         * The function itself.
         */
        func: T;

        /**
         * A reasonable name for the function.
         */
        name?: string;
        /**
         * What the `name` field means. The empty string means that Lua did not find
         * a name for the function.
         */
        namewhat: 'global' | 'local' | 'method' | 'field' | '';

        source: string;
        /**
         * A short version of source (up to 60 characters), useful for error
         * messages.
         */
        short_src: string;
        linedefined: number;
        lastlinedefined: number;
        /**
         * What this function is.
         */
        what: 'Lua' | 'C' | 'main';

        currentline: number;

        /**
         * Number of upvalues of that function.
         */
        nups: number;
    }

    /**
     * Returns a table with information about a function. You can give the
     * function directly or you can give a number as the value of f, which means
     * the function running at level f of the call stack of the given thread:
     * level 0 is the current function (getinfo itself); level 1 is the function
     * that called getinfo (except for tail calls, which do not count on the
     * stack); and so on. If f is a number larger than the number of active
     * functions, then getinfo returns nil.
     *
     * The returned table can contain all the fields returned by lua_getinfo, with
     * the string what describing which fields to fill in. The default for what is
     * to get all information available, except the table of valid lines. If
     * present, the option 'f' adds a field named func with the function itself.
     * If present, the option 'L' adds a field named activelines with the table of
     * valid lines.
     *
     * For instance, the expression debug.getinfo(1,"n").name returns a name for
     * the current function, if a reasonable name can be found, and the expression
     * debug.getinfo(print) returns a table with all available information about
     * the print function.
     */
    function getinfo<T extends Function>(f: T): FunctionInfo<T>;
    function getinfo<T extends Function>(f: T, what: string): Partial<FunctionInfo<T>>;
    function getinfo<T extends Function>(thread: LuaThread, f: T): FunctionInfo<T>;
    function getinfo<T extends Function>(
        thread: LuaThread,
        f: T,
        what: string
    ): Partial<FunctionInfo<T>>;
    function getinfo(f: number): FunctionInfo | undefined;
    function getinfo(f: number, what: string): Partial<FunctionInfo> | undefined;
    function getinfo(thread: LuaThread, f: number): FunctionInfo | undefined;
    function getinfo(thread: LuaThread, f: number, what: string): Partial<FunctionInfo> | undefined;

    /**
     * Returns the metatable of the given value or nil if it does not have a
     * metatable.
     */
    function getmetatable<T extends any>(value: T): LuaMetatable<T> | undefined;

    /**
     * Returns the registry table (see §4.5).
     */
    function getregistry(): Record<string, any>;

    /**
     * This function returns the name and the value of the upvalue with index up
     * of the function f. The function returns nil if there is no upvalue with the
     * given index.
     *
     * Variable names starting with '(' (open parenthesis) represent variables
     * with no known names (variables from chunks saved without debug
     * information).
     */
    function getupvalue(f: Function, up: number): LuaMultiReturn<[string, any] | []>;

    /**
     * Returns the Lua value associated to u. If u is not a full userdata, returns
     * nil.
     */
    function getuservalue(u: LuaUserdata): any;

    /**
     * Sets the given function as a hook. The string mask and the number count
     * describe when the hook will be called. The string mask may have any
     * combination of the following characters, with the given meaning:
     *
     * * 'c': the hook is called every time Lua calls a function;
     * * 'r': the hook is called every time Lua returns from a function;
     * * 'l': the hook is called every time Lua enters a new line of code.
     *
     * Moreover, with a count different from zero, the hook is called also after
     * every count instructions.
     *
     * When called without arguments, debug.sethook turns off the hook.
     *
     * When the hook is called, its first parameter is a string describing the
     * event that has triggered its call: "call" (or "tail call"), "return",
     * "line", and "count". For line events, the hook also gets the new line
     * number as its second parameter. Inside a hook, you can call getinfo with
     * level 2 to get more information about the running function (level 0 is the
     * getinfo function, and level 1 is the hook function).
     */
    function sethook(): void;
    function sethook(
        hook: (event: 'call' | 'return' | 'line' | 'count', line?: number) => any,
        mask: string,
        count?: number
    ): void;
    function sethook(
        thread: LuaThread,
        hook: (event: 'call' | 'return' | 'line' | 'count', line?: number) => any,
        mask: string,
        count?: number
    ): void;

    /**
     * This function assigns the value value to the local variable with index
     * local of the function at level level of the stack. The function returns nil
     * if there is no local variable with the given index, and raises an error
     * when called with a level out of range. (You can call getinfo to check
     * whether the level is valid.) Otherwise, it returns the name of the local
     * variable.
     *
     * See debug.getlocal for more information about variable indices and names.
     */
    function setlocal(level: number, local: number, value: any): string | undefined;
    function setlocal(
        thread: LuaThread,
        level: number,
        local: number,
        value: any
    ): string | undefined;

    /**
     * Sets the metatable for the given value to the given table (which can be
     * nil). Returns value.
     */
    function setmetatable<
        T extends object,
        TIndex extends object | ((this: T, key: any) => any) | undefined = undefined
    >(
        value: T,
        table?: LuaMetatable<T, TIndex> | null
    ): TIndex extends (this: T, key: infer TKey) => infer TValue
        ? T & { [K in TKey & string]: TValue }
        : TIndex extends object
        ? T & TIndex
        : T;

    /**
     * This function assigns the value value to the upvalue with index up of the
     * function f. The function returns nil if there is no upvalue with the given
     * index. Otherwise, it returns the name of the upvalue.
     */
    function setupvalue(f: Function, up: number, value: any): string | undefined;

    /**
     * Sets the given value as the Lua value associated to the given udata. udata
     * must be a full userdata.
     *
     * Returns udata.
     */
    function setuservalue(udata: LuaUserdata, value: any): LuaUserdata;

    /**
     * If message is present but is neither a string nor nil, this function
     * returns message without further processing. Otherwise, it returns a string
     * with a traceback of the call stack. The optional message string is appended
     * at the beginning of the traceback. An optional level number tells at which
     * level to start the traceback (default is 1, the function calling
     * traceback).
     */
    function traceback(message?: string | null, level?: number | null): string;
    function traceback(thread?: LuaThread, message?: string | null, level?: number | null): string;
    function traceback<T>(message: T): T;
    function traceback<T>(thread: LuaThread, message: T): T;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.1

/// <reference lib="es2015.iterable" />

/** @noSelfInFile */

type LuaThread = { readonly __internal__: unique symbol };
type LuaUserdata = { readonly __internal__: unique symbol };

/**
 * A global variable (not a function) that holds a string containing the running
 * Lua version.
 */
declare const _VERSION:
    | ('Lua 5.0' | 'Lua 5.0.1' | 'Lua 5.0.2' | 'Lua 5.0.3')
    | 'Lua 5.1'
    | 'Lua 5.2'
    | 'Lua 5.3'
    | 'Lua 5.4';

/**
 * A global variable (not a function) that holds the global environment (see
 * §2.2). Lua itself does not use this variable; changing its value does not
 * affect any environment, nor vice versa.
 */
declare const _G: typeof globalThis;

/**
 * Calls error if the value of its argument `v` is false (i.e., nil or false);
 * otherwise, returns all its arguments. In case of error, `message` is the
 * error object; when absent, it defaults to "assertion failed!"
 */
declare function assert<V>(v: V): Exclude<V, undefined | null | false>;
declare function assert<V, A extends any[]>(
    v: V,
    ...args: A
): LuaMultiReturn<[Exclude<V, undefined | null | false>, ...A]>;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Performs a full garbage-collection cycle. This is the default option.
 */
declare function collectgarbage(opt?: 'collect'): void;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Stops automatic execution of the garbage collector. The collector will run
 * only when explicitly invoked, until a call to restart it.
 */
declare function collectgarbage(opt: 'stop'): void;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Restarts automatic execution of the garbage collector.
 */
declare function collectgarbage(opt: 'restart'): void;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Sets arg as the new value for the pause of the collector (see §2.5). Returns
 * the previous value for pause.
 */
declare function collectgarbage(opt: 'setpause', arg: number): number;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Sets arg as the new value for the step multiplier of the collector (see
 * §2.5). Returns the previous value for step.
 */
declare function collectgarbage(opt: 'setstepmul', arg: number): number;

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Performs a garbage-collection step. The step "size" is controlled by arg.
 * With a zero value, the collector will perform one basic (indivisible) step.
 * For non-zero values, the collector will perform as if that amount of memory
 * (in KBytes) had been allocated by Lua. Returns true if the step finished a
 * collection cycle.
 */
declare function collectgarbage(opt: 'step', arg: number): boolean;

/**
 * Opens the named file and executes its contents as a Lua chunk. When called
 * without arguments, dofile executes the contents of the standard input
 * (stdin). Returns all values returned by the chunk. In case of errors, dofile
 * propagates the error to its caller (that is, dofile does not run in protected
 * mode).
 */
declare function dofile(filename?: string): any;

/**
 * Terminates the last protected function called and returns message as the
 * error object. Function error never returns.
 *
 * Usually, error adds some information about the error position at the
 * beginning of the message, if the message is a string. The level argument
 * specifies how to get the error position. With level 1 (the default), the
 * error position is where the error function was called. Level 2 points the
 * error to where the function that called error was called; and so on. Passing
 * a level 0 avoids the addition of error position information to the message.
 */
declare function error(message: string, level?: number): never;

/**
 * If object does not have a metatable, returns nil. Otherwise, if the object's
 * metatable has a __metatable field, returns the associated value. Otherwise,
 * returns the metatable of the given object.
 */
declare function getmetatable<T>(object: T): LuaMetatable<T> | undefined;

/**
 * Returns three values (an iterator function, the table t, and 0) so that the
 * construction
 *
 * `for i,v in ipairs(t) do body end`
 *
 * will iterate over the key–value pairs (1,t[1]), (2,t[2]), ..., up to the
 * first nil value.
 */
declare function ipairs<T>(
    t: Record<number, T>
): LuaIterable<LuaMultiReturn<[number, NonNullable<T>]>>;

/**
 * Allows a program to traverse all fields of a table. Its first argument is a
 * table and its second argument is an index in this table. next returns the
 * next index of the table and its associated value. When called with nil as its
 * second argument, next returns an initial index and its associated value. When
 * called with the last index, or with nil in an empty table, next returns nil.
 * If the second argument is absent, then it is interpreted as nil. In
 * particular, you can use next(t) to check whether a table is empty.
 *
 * The order in which the indices are enumerated is not specified, even for
 * numeric indices. (To traverse a table in numerical order, use a numerical
 * for.)
 *
 * The behavior of next is undefined if, during the traversal, you assign any
 * value to a non-existent field in the table. You may however modify existing
 * fields. In particular, you may clear existing fields.
 */
declare function next(table: object, index?: any): LuaMultiReturn<[any, any] | []>;

/**
 * If t has a metamethod __pairs, calls it with t as argument and returns the
 * first three results from the call. Otherwise, returns three values: the next
 * function, the table t, and nil, so that the construction
 *
 * `for k,v in pairs(t) do body end`
 *
 * will iterate over all key–value pairs of table t.
 *
 * See function next for the caveats of modifying the table during its
 * traversal.
 */
declare function pairs<TKey extends AnyNotNil, TValue>(
    t: LuaTable<TKey, TValue>
): LuaIterable<LuaMultiReturn<[TKey, NonNullable<TValue>]>>;
declare function pairs<T>(t: T): LuaIterable<LuaMultiReturn<[keyof T, NonNullable<T[keyof T]>]>>;

/**
 * Calls function f with the given arguments in protected mode. This means that
 * any error inside f is not propagated; instead, pcall catches the error and
 * returns a status code. Its first result is the status code (a boolean), which
 * is true if the call succeeds without errors. In such case, pcall also returns
 * all results from the call, after this first result. In case of any error,
 * pcall returns false plus the error message.
 */
declare function pcall<This, Args extends any[], R>(
    f: (this: This, ...args: Args) => R,
    context: This,
    ...args: Args
): LuaMultiReturn<[true, R] | [false, string]>;

declare function pcall<A extends any[], R>(
    f: (this: void, ...args: A) => R,
    ...args: A
): LuaMultiReturn<[true, R] | [false, string]>;

/**
 * Receives any number of arguments and prints their values to stdout, using the
 * tostring function to convert each argument to a string. print is not intended
 * for formatted output, but only as a quick way to show a value, for instance
 * for debugging. For complete control over the output, use string.format and
 * io.write.
 */
declare function print(...args: any[]): void;

/**
 * Checks whether v1 is equal to v2, without invoking the __eq metamethod.
 * Returns a boolean.
 */
declare function rawequal<T>(v1: T, v2: T): boolean;

/**
 * Gets the real value of table[index], without invoking the __index metamethod.
 * table must be a table; index may be any value.
 */
declare function rawget<T extends object, K extends keyof T>(table: T, index: K): T[K];

/**
 * Returns the length of the object v, which must be a table or a string,
 * without invoking the __len metamethod. Returns an integer.
 */
declare function rawlen(v: object | string): number;

/**
 * Sets the real value of table[index] to value, without invoking the __newindex
 * metamethod. table must be a table, index any value different from nil and
 * NaN, and value any Lua value.
 *
 * This function returns table.
 */
declare function rawset<T extends object, K extends keyof T>(table: T, index: K, value: T[K]): T;

/**
 * If index is a number, returns all arguments after argument number index; a
 * negative number indexes from the end (-1 is the last argument). Otherwise,
 * index must be the string "#", and select returns the total number of extra
 * arguments it received.
 */
declare function select<T>(index: number, ...args: T[]): LuaMultiReturn<T[]>;

/**
 * If index is a number, returns all arguments after argument number index; a
 * negative number indexes from the end (-1 is the last argument). Otherwise,
 * index must be the string "#", and select returns the total number of extra
 * arguments it received.
 */
declare function select<T>(index: '#', ...args: T[]): number;

/**
 * Sets the metatable for the given table. (To change the metatable of other
 * types from Lua code, you must use the debug library (§6.10).) If metatable is
 * nil, removes the metatable of the given table. If the original metatable has
 * a __metatable field, raises an error.
 *
 * This function returns table.
 */
declare function setmetatable<
    T extends object,
    TIndex extends object | ((this: T, key: any) => any) | undefined = undefined
>(
    table: T,
    metatable?: LuaMetatable<T, TIndex> | null
): TIndex extends (this: T, key: infer TKey) => infer TValue
    ? T & { [K in TKey & string]: TValue }
    : TIndex extends object
    ? T & TIndex
    : T;

/**
 * When called with no base, tonumber tries to convert its argument to a number.
 * If the argument is already a number or a string convertible to a number, then
 * tonumber returns this number; otherwise, it returns nil.
 *
 * The conversion of strings can result in integers or floats, according to the
 * lexical conventions of Lua (see §3.1). (The string may have leading and
 * trailing spaces and a sign.)
 *
 * When called with base, then e must be a string to be interpreted as an
 * integer numeral in that base. The base may be any integer between 2 and 36,
 * inclusive. In bases above 10, the letter 'A' (in either upper or lower case)
 * represents 10, 'B' represents 11, and so forth, with 'Z' representing 35. If
 * the string e is not a valid numeral in the given base, the function returns
 * nil.
 */
declare function tonumber(e: any, base?: number): number | undefined;

/**
 * Receives a value of any type and converts it to a string in a human-readable
 * format. (For complete control of how numbers are converted, use
 * string.format.)
 *
 * If the metatable of v has a __tostring field, then tostring calls the
 * corresponding value with v as argument, and uses the result of the call as
 * its result.
 */
declare function tostring(v: any): string;

/**
 * Returns the type of its only argument, coded as a string.
 */
declare function type(
    v: any
): 'nil' | 'number' | 'string' | 'boolean' | 'table' | 'function' | 'thread' | 'userdata';

// Based on https://www.lua.org/manual/5.3/manual.html#6.8

/** @noSelfInFile */

/**
 * The I/O library provides two different styles for file manipulation. The
 * first one uses implicit file handles; that is, there are operations to set a
 * default input file and a default output file, and all input/output operations
 * are over these default files. The second style uses explicit file handles.
 *
 * When using implicit file handles, all operations are supplied by table io.
 * When using explicit file handles, the operation io.open returns a file handle
 * and then all operations are supplied as methods of the file handle.
 *
 * The table io also provides three predefined file handles with their usual
 * meanings from C: io.stdin, io.stdout, and io.stderr. The I/O library never
 * closes these files.
 *
 * Unless otherwise stated, all I/O functions return nil on failure (plus an
 * error message as a second result and a system-dependent error code as a third
 * result) and some value different from nil on success. On non-POSIX systems,
 * the computation of the error message and error code in case of errors may be
 * not thread safe, because they rely on the global C variable errno.
 */
declare namespace io {
    /**
     * Equivalent to file:close(). Without a file, closes the default output file.
     */
    function close(file?: LuaFile): boolean;

    /**
     * Equivalent to io.output():flush().
     */
    function flush(): boolean;

    /**
     * When called with a file name, it opens the named file (in text mode), and
     * sets its handle as the default input file. When called with a file handle,
     * it simply sets this file handle as the default input file. When called
     * without parameters, it returns the current default input file.
     *
     * In case of errors this function raises the error, instead of returning an
     * error code.
     */
    function input(file?: string | LuaFile): LuaFile;

    /**
     * Opens the given file name in read mode and returns an iterator function
     * that works like file:lines(···) over the opened file. When the iterator
     * function detects the end of file, it returns no values (to finish the loop)
     * and automatically closes the file.
     *
     * The call io.lines() (with no file name) is equivalent to
     * io.input():lines("*l"); that is, it iterates over the lines of the default
     * input file. In this case it does not close the file when the loop ends.
     *
     * In case of errors this function raises the error, instead of returning an
     * error code.
     */
    function lines<T extends FileReadFormat[]>(
        filename?: string,
        ...formats: T
    ): LuaIterable<
        LuaMultiReturn<[] extends T ? [string] : { [P in keyof T]: FileReadFormatToType<T[P]> }>
    >;

    /**
     * This function opens a file, in the mode specified in the string mode. In
     * case of success, it returns a new file handle.
     *
     * The mode string can be any of the following:
     *
     * * "r": read mode (the default);
     * * "w": write mode;
     * * "a": append mode;
     * * "r+": update mode, all previous data is preserved;
     * * "w+": update mode, all previous data is erased;
     * * "a+": append update mode, previous data is preserved, writing is only
     *   allowed at the end of file.
     *
     * The mode string can also have a 'b' at the end, which is needed in some
     * systems to open the file in binary mode.
     */
    function open(
        filename: string,
        mode?: string
    ): LuaMultiReturn<[LuaFile] | [undefined, string, number]>;

    /**
     * Similar to io.input, but operates over the default output file.
     */
    function output(file?: string | LuaFile): LuaFile;

    /**
     * This function is system dependent and is not available on all platforms.
     *
     * Starts program prog in a separated process and returns a file handle that
     * you can use to read data from this program (if mode is "r", the default) or
     * to write data to this program (if mode is "w").
     */
    function popen(prog: string, mode?: 'r' | 'w'): LuaMultiReturn<[LuaFile] | [undefined, string]>;

    /**
     * Equivalent to io.input():read(···).
     */
    function read(): io.FileReadFormatToType<io.FileReadLineFormat> | undefined;
    function read<T extends io.FileReadFormat>(format: T): io.FileReadFormatToType<T> | undefined;
    function read<T extends io.FileReadFormat[]>(
        ...formats: T
    ): LuaMultiReturn<{ [P in keyof T]?: io.FileReadFormatToType<T[P]> }>;

    /**
     * Predefined file handle for standard error stream. The I/O library never closes this file.
     */
    const stderr: LuaFile;

    /**
     * Predefined file handle for standard input stream. The I/O library never closes this file.
     */
    const stdin: LuaFile;

    /**
     * Predefined file handle for standard output stream. The I/O library never closes this file.
     */
    const stdout: LuaFile;

    /**
     * In case of success, returns a handle for a temporary file. This file is
     * opened in update mode and it is automatically removed when the program
     * ends.
     */
    function tmpfile(): LuaFile;

    /**
     * Checks whether obj is a valid file handle. Returns the string "file" if obj
     * is an open file handle, "closed file" if obj is a closed file handle, or
     * nil if obj is not a file handle.
     */
    function type(obj: any): 'file' | 'closed file' | undefined;

    /**
     * Equivalent to io.output():write(···).
     */
    function write(...args: (string | number)[]): LuaMultiReturn<[LuaFile] | [undefined, string]>;

    type FileReadFormatToType<T> = T extends FileReadNumberFormat ? number : string;
}

interface LuaFile {
    /**
     * Closes file. Note that files are automatically closed when their handles
     * are garbage collected, but that takes an unpredictable amount of time to
     * happen.
     *
     * When closing a file handle created with io.popen, file:close returns the
     * same values returned by os.execute.
     */
    close(): boolean;

    /**
     * Saves any written data to file.
     */
    flush(): boolean;

    /**
     * Returns an iterator function that, each time it is called, reads the file
     * according to the given formats. When no format is given, uses "l" as a
     * default. As an example, the construction
     *
     * `for c in file:lines(1) do body end`
     *
     * will iterate over all characters of the file, starting at the current
     * position. Unlike io.lines, this function does not close the file when the
     * loop ends.
     *
     * In case of errors this function raises the error, instead of returning an
     * error code.
     */
    lines<T extends io.FileReadFormat[]>(
        ...formats: T
    ): LuaIterable<
        LuaMultiReturn<[] extends T ? [string] : { [P in keyof T]: io.FileReadFormatToType<T[P]> }>
    >;

    /**
     * Reads the file file, according to the given formats, which specify what to
     * read. For each format, the function returns a string or a number with the
     * characters read, or nil if it cannot read data with the specified format.
     * (In this latter case, the function does not read subsequent formats.) When
     * called without formats, it uses a default format that reads the next line
     * (see below).
     *
     * The available formats are
     *
     * * "n": reads a numeral and returns it as a float or an integer, following
     *   the lexical conventions of Lua. (The numeral may have leading spaces and
     *   a sign.) This format always reads the longest input sequence that is a
     *   valid prefix for a numeral; if that prefix does not form a valid numeral
     *   (e.g., an empty string, "0x", or "3.4e-"), it is discarded and the
     *   function returns nil.
     * * "a": reads the whole file, starting at the current position. On end of
     *   file, it returns the empty string.
     * * "l": reads the next line skipping the end of line, returning nil on end
     *   of file. This is the default format.
     * * "L": reads the next line keeping the end-of-line character (if present),
     *   returning nil on end of file.
     * * number: reads a string with up to this number of bytes, returning nil on
     *   end of file. If number is zero, it reads nothing and returns an empty
     *   string, or nil on end of file.
     *
     * The formats "l" and "L" should be used only for text files.
     */
    read(): io.FileReadFormatToType<io.FileReadLineFormat> | undefined;
    read<T extends io.FileReadFormat>(format: T): io.FileReadFormatToType<T> | undefined;
    read<T extends io.FileReadFormat[]>(
        ...formats: T
    ): LuaMultiReturn<{ [P in keyof T]?: io.FileReadFormatToType<T[P]> }>;

    /**
     * Sets and geionts the file position, measured from the beginning of the
     * file, to the posit given by offset plus a base specified by the string
     * whence, as follows:
     *
     * * "set": base is position 0 (beginning of the file);
     * * "cur": base is current position;
     * * "end": base is end of file;
     *
     * In case of success, seek returns the final file position, measured in bytes
     * from the beginning of the file. If seek fails, it returns nil, plus a
     * string describing the error.
     *
     * The default value for whence is "cur", and for offset is 0. Therefore, the
     * call file:seek() returns the current file position, without changing it;
     * the call file:seek("set") sets the position to the beginning of the file
     * (and returns 0); and the call file:seek("end") sets the position to the end
     * of the file, and returns its size.
     */
    seek(
        whence?: 'set' | 'cur' | 'end',
        offset?: number
    ): LuaMultiReturn<[number] | [undefined, string]>;

    /**
     * Sets the buffering mode for an output file. There are three available
     * modes:
     *
     * * "no": no buffering; the result of any output operation appears
     *   immediately.
     * * "full": full buffering; output operation is performed only when the
     *   buffer is full or when you explicitly flush the file (see io.flush).
     * * "line": line buffering; output is buffered until a newline is output or
     *   there is any input from some special files (such as a terminal device).
     *   For the last two cases, size specifies the size of the buffer, in bytes.
     *   The default is an appropriate size.
     */
    setvbuf(mode: 'no' | 'full' | 'line', size?: number): void;

    /**
     * Writes the value of each of its arguments to file. The arguments must be
     * strings or numbers.
     *
     * In case of success, this function returns file. Otherwise it returns nil
     * plus a string describing the error.
     */
    write(...args: (string | number)[]): LuaMultiReturn<[LuaFile] | [undefined, string]>;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.7

/** @noSelfInFile */

/**
 * This library provides basic mathematical functions. It provides all its
 * functions and constants inside the table math. Functions with the annotation
 * "integer/float" give integer results for integer arguments and float results
 * for float (or mixed) arguments. Rounding functions (math.ceil, math.floor,
 * and math.modf) return an integer when the result fits in the range of an
 * integer, or a float otherwise.
 */
declare namespace math {
    /**
     * Returns the absolute value of x. (integer/float)
     */
    function abs(x: number): number;

    /**
     * Returns the arc cosine of x (in radians).
     */
    function acos(x: number): number;

    /**
     * Returns the arc sine of x (in radians).
     */
    function asin(x: number): number;

    /**
     * Returns the smallest integral value larger than or equal to x.
     */
    function ceil(x: number): number;

    /**
     * Returns the cosine of x (assumed to be in radians).
     */
    function cos(x: number): number;

    /**
     * Converts the angle x from radians to degrees.
     */
    function deg(x: number): number;

    /**
     * Returns the value ex (where e is the base of natural logarithms).
     */
    function exp(x: number): number;

    /**
     * Returns the largest integral value smaller than or equal to x.
     */
    function floor(x: number): number;

    /**
     * Returns the remainder of the division of x by y that rounds the quotient
     * towards zero. (integer/float)
     */
    function fmod(x: number, y: number): number;

    /**
     * The float value HUGE_VAL, a value larger than any other numeric value.
     */
    const huge: number;

    /**
     * Returns the argument with the maximum value, according to the Lua operator
     * <. (integer/float)
     */
    function max(x: number, ...numbers: number[]): number;

    /**
     * Returns the argument with the minimum value, according to the Lua operator
     * <. (integer/float)
     */
    function min(x: number, ...numbers: number[]): number;

    /**
     * Returns the integral part of x and the fractional part of x. Its second
     * result is always a float.
     */
    function modf(x: number): LuaMultiReturn<[number, number]>;

    /**
     * The value of π.
     */
    const pi: number;

    /**
     * Converts the angle x from degrees to radians.
     */
    function rad(x: number): number;

    /**
     * Returns the sine of x (assumed to be in radians).
     */
    function sin(x: number): number;

    /**
     * Returns the square root of x. (You can also use the expression x^0.5 to
     * compute this value.)
     */
    function sqrt(x: number): number;

    /**
     * Returns the tangent of x (assumed to be in radians).
     */
    function tan(x: number): number;
}

// Based on https://www.lua.org/manual/5.3/manual.html#2.4

interface LuaMetatable<
    T,
    TIndex extends object | ((this: T, key: any) => any) | undefined =
        | object
        | ((this: T, key: any) => any)
        | undefined
> {
    /**
     * the addition (+) operation. If any operand for an addition is not a number
     * (nor a string coercible to a number), Lua will try to call a metamethod.
     * First, Lua will check the first operand (even if it is valid). If that
     * operand does not define a metamethod for __add, then Lua will check the
     * second operand. If Lua can find a metamethod, it calls the metamethod with
     * the two operands as arguments, and the result of the call (adjusted to one
     * value) is the result of the operation. Otherwise, it raises an error.
     */
    __add?(this: T, operand: any): any;

    /**
     * the subtraction (-) operation. Behavior similar to the addition operation.
     */
    __sub?(this: T, operand: any): any;

    /**
     * the multiplication (*) operation. Behavior similar to the addition
     * operation.
     */
    __mul?(this: T, operand: any): any;

    /**
     * the division (/) operation. Behavior similar to the addition operation.
     */
    __div?(this: T, operand: any): any;

    /**
     * the modulo (%) operation. Behavior similar to the addition operation.
     */
    __mod?(this: T, operand: any): any;

    /**
     * the exponentiation (^) operation. Behavior similar to the addition
     * operation.
     */
    __pow?(this: T, operand: any): any;

    /**
     * the negation (unary -) operation. Behavior similar to the addition
     * operation.
     */
    __unm?(this: T, operand: any): any;

    /**
     * the concatenation (..) operation. Behavior similar to the addition
     * operation, except that Lua will try a metamethod if any operand is neither
     * a string nor a number (which is always coercible to a string).
     */
    __concat?(this: T, operand: any): any;

    /**
     * the length (#) operation. If the object is not a string, Lua will try its
     * metamethod. If there is a metamethod, Lua calls it with the object as
     * argument, and the result of the call (always adjusted to one value) is the
     * result of the operation. If there is no metamethod but the object is a
     * table, then Lua uses the table length operation (see §3.4.7). Otherwise,
     * Lua raises an error.
     */
    __len?(this: T): any;

    /**
     * the equal (==) operation. Behavior similar to the addition operation,
     * except that Lua will try a metamethod only when the values being compared
     * are either both tables or both full userdata and they are not primitively
     * equal. The result of the call is always converted to a boolean.
     */
    __eq?(this: T, operand: any): boolean;

    /**
     * the less than (<) operation. Behavior similar to the addition operation,
     * except that Lua will try a metamethod only when the values being compared
     * are neither both numbers nor both strings. The result of the call is always
     * converted to a boolean.
     */
    __lt?(this: T, operand: any): boolean;

    /**
     * the less equal (<=) operation. Unlike other operations, the less-equal
     * operation can use two different events. First, Lua looks for the __le
     * metamethod in both operands, like in the less than operation. If it cannot
     * find such a metamethod, then it will try the __lt metamethod, assuming that
     * a <= b is equivalent to not (b < a). As with the other comparison
     * operators, the result is always a boolean. (This use of the __lt event can
     * be removed in future versions; it is also slower than a real __le
     * metamethod.)
     */
    __le?(this: T, operand: any): boolean;

    /**
     * The indexing access table[key]. This event happens when table is not a
     * table or when key is not present in table. The metamethod is looked up in
     * table.
     *
     * Despite the name, the metamethod for this event can be either a function or
     * a table. If it is a function, it is called with table and key as arguments,
     * and the result of the call (adjusted to one value) is the result of the
     * operation. If it is a table, the final result is the result of indexing
     * this table with key. (This indexing is regular, not raw, and therefore can
     * trigger another metamethod.)
     */
    __index?: TIndex;

    /**
     * The indexing assignment table[key] = value. Like the index event, this
     * event happens when table is not a table or when key is not present in
     * table. The metamethod is looked up in table.
     *
     * Like with indexing, the metamethod for this event can be either a function
     * or a table. If it is a function, it is called with table, key, and value as
     * arguments. If it is a table, Lua does an indexing assignment to this table
     * with the same key and value. (This assignment is regular, not raw, and
     * therefore can trigger another metamethod.)
     *
     * Whenever there is a __newindex metamethod, Lua does not perform the
     * primitive assignment. (If necessary, the metamethod itself can call rawset
     * to do the assignment.)
     */
    __newindex?: object | ((this: T, key: any, value: any) => void);

    /**
     * The call operation func(args). This event happens when Lua tries to call a
     * non-function value (that is, func is not a function). The metamethod is
     * looked up in func. If present, the metamethod is called with func as its
     * first argument, followed by the arguments of the original call (args). All
     * results of the call are the result of the operation. (This is the only
     * metamethod that allows multiple results.)
     */
    __call?(this: T, ...args: any[]): any;

    /**
     * If the metatable of v has a __tostring field, then tostring calls the
     * corresponding value with v as argument, and uses the result of the call as
     * its result.
     */
    __tostring?(this: T): string;

    /**
     * If this field is a string containing the character 'k', the keys in the
     * table are weak. If it contains 'v', the values in the table are weak.
     */
    __mode?: 'k' | 'v' | 'kv';

    /**
     * If the object's metatable has this field, `getmetatable` returns the
     * associated value.
     */
    __metatable?: any;

    /**
     * Userdata finalizer code. When userdata is set to be garbage collected, if
     * the metatable has a __gc field pointing to a function, that function is
     * first invoked, passing the userdata to it. The __gc metamethod is not
     * called for tables.
     */
    __gc?(this: T): void;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.3

/** @noSelfInFile */

/**
 * Loads the given module. The function starts by looking into the
 * package.loaded table to determine whether modname is already loaded. If it
 * is, then require returns the value stored at package.loaded[modname].
 * Otherwise, it tries to find a loader for the module.
 *
 * To find a loader, require is guided by the package.searchers sequence. By
 * changing this sequence, we can change how require looks for a module. The
 * following explanation is based on the default configuration for
 * package.searchers.
 *
 * First require queries package.preload[modname]. If it has a value, this value
 * (which must be a function) is the loader. Otherwise require searches for a
 * Lua loader using the path stored in package.path. If that also fails, it
 * searches for a C loader using the path stored in package.cpath. If that also
 * fails, it tries an all-in-one loader (see package.searchers).
 *
 * Once a loader is found, require calls the loader with two arguments: modname
 * and an extra value dependent on how it got the loader. (If the loader came
 * from a file, this extra value is the file name.) If the loader returns any
 * non-nil value, require assigns the returned value to package.loaded[modname].
 * If the loader does not return a non-nil value and has not assigned any value
 * to package.loaded[modname], then require assigns true to this entry. In any
 * case, require returns the final value of package.loaded[modname].
 *
 * If there is any error loading or running the module, or if it cannot find any
 * loader for the module, then require raises an error.
 */
declare function require(modname: string): any;

/**
 * The package library provides basic facilities for loading modules in Lua. It
 * exports one function directly in the global environment: require. Everything
 * else is exported in a table package.
 */
declare namespace package {
    /**
     * A string describing some compile-time configurations for packages. This
     * string is a sequence of lines:
     * * The first line is the directory separator string. Default is '\' for
     *   Windows and '/' for all other systems.
     * * The second line is the character that separates templates in a path.
     *   Default is ';'.
     * * The third line is the string that marks the substitution points in a
     *   template. Default is '?'.
     * * The fourth line is a string that, in a path in Windows, is replaced by
     *   the executable's directory. Default is '!'.
     * * The fifth line is a mark to ignore all text after it when building the
     *   luaopen_ function name. Default is '-'.
     */
    var config: string;

    /**
     * The path used by require to search for a C loader.
     *
     * Lua initializes the C path package.cpath in the same way it initializes the
     * Lua path package.path, using the environment variable LUA_CPATH_5_3, or the
     * environment variable LUA_CPATH, or a default path defined in luaconf.h.
     */
    var cpath: string;

    /**
     * A table used by require to control which modules are already loaded. When
     * you require a module modname and package.loaded[modname] is not false,
     * require simply returns the value stored there.
     *
     * This variable is only a reference to the real table; assignments to this
     * variable do not change the table used by require.
     */
    const loaded: Record<string, any>;

    /**
     * Dynamically links the host program with the C library libname.
     *
     * If funcname is "*", then it only links with the library, making the symbols
     * exported by the library available to other dynamically linked libraries.
     * Otherwise, it looks for a function funcname inside the library and returns
     * this function as a C function. So, funcname must follow the lua_CFunction
     * prototype (see lua_CFunction).
     *
     * This is a low-level function. It completely bypasses the package and module
     * system. Unlike require, it does not perform any path searching and does not
     * automatically adds extensions. libname must be the complete file name of
     * the C library, including if necessary a path and an extension. funcname
     * must be the exact name exported by the C library (which may depend on the C
     * compiler and linker used).
     *
     * This function is not supported by Standard C. As such, it is only available
     * on some platforms (Windows, Linux, Mac OS X, Solaris, BSD, plus other Unix
     * systems that support the dlfcn standard).
     */
    function loadlib(
        libname: string,
        funcname: string
    ): [Function] | [undefined, string, 'open' | 'init'];

    /**
     * The path used by require to search for a Lua loader.
     *
     * At start-up, Lua initializes this variable with the value of the
     * environment variable LUA_PATH_5_3 or the environment variable LUA_PATH or
     * with a default path defined in luaconf.h, if those environment variables
     * are not defined. Any ";;" in the value of the environment variable is
     * replaced by the default path.
     */
    var path: string;

    /**
     * A table to store loaders for specific modules (see require).
     *
     * This variable is only a reference to the real table; assignments to this
     * variable do not change the table used by require.
     */
    const preload: Record<string, (modname: string, fileName?: string) => any>;

    /**
     * Searches for the given name in the given path.
     *
     * A path is a string containing a sequence of templates separated by
     * semicolons. For each template, the function replaces each interrogation
     * mark (if any) in the template with a copy of name wherein all occurrences
     * of sep (a dot, by default) were replaced by rep (the system's directory
     * separator, by default), and then tries to open the resulting file name.
     *
     * For instance, if the path is the string
     *
     * `./?.lua;./?.lc;/usr/local/?/init.lua`
     *
     * the search for the name foo.a will try to open the files ./foo/a.lua,
     * ./foo/a.lc, and /usr/local/foo/a/init.lua, in that order.
     *
     * Returns the resulting name of the first file that it can open in read mode
     * (after closing the file), or nil plus an error message if none succeeds.
     * (This error message lists all file names it tried to open.)
     */
    function searchpath(name: string, path: string, sep?: string, rep?: string): string;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.9

/** @noSelfInFile */

interface LuaDateInfo {
    year: number;
    month: number;
    day: number;
    hour?: number;
    min?: number;
    sec?: number;
    isdst?: boolean;
}

interface LuaDateInfoResult {
    year: number;
    month: number;
    day: number;
    hour: number;
    min: number;
    sec: number;
    isdst: boolean;
    yday: number;
    wday: number;
}

/**
 * Operating System Facilities
 */
declare namespace os {
    /**
     * Returns an approximation of the amount in seconds of CPU time used by the
     * program.
     */
    function clock(): number;

    /**
     * Returns a string or a table containing date and time, formatted according
     * to the given string format.
     *
     * If the time argument is present, this is the time to be formatted (see the
     * os.time function for a description of this value). Otherwise, date formats
     * the current time.
     *
     * If format starts with '!', then the date is formatted in Coordinated
     * Universal Time. After this optional character, if format is the string
     * "*t", then date returns a table with the following fields: year, month
     * (1–12), day (1–31), hour (0–23), min (0–59), sec (0–61), wday (weekday,
     * 1–7, Sunday is 1), yday (day of the year, 1–366), and isdst (daylight
     * saving flag, a boolean). This last field may be absent if the information
     * is not available.
     *
     * If format is not "*t", then date returns the date as a string, formatted
     * according to the same rules as the ISO C function strftime.
     *
     * When called without arguments, date returns a reasonable date and time
     * representation that depends on the host system and on the current locale.
     * (More specifically, os.date() is equivalent to os.date("%c").)
     *
     * On non-POSIX systems, this function may be not thread safe because of its
     * reliance on C function gmtime and C function localtime.
     */
    function date(format?: string, time?: number): string;

    /**
     * Returns a string or a table containing date and time, formatted according
     * to the given string format.
     *
     * If the time argument is present, this is the time to be formatted (see the
     * os.time function for a description of this value). Otherwise, date formats
     * the current time.
     *
     * If format starts with '!', then the date is formatted in Coordinated
     * Universal Time. After this optional character, if format is the string
     * "*t", then date returns a table with the following fields: year, month
     * (1–12), day (1–31), hour (0–23), min (0–59), sec (0–61), wday (weekday,
     * 1–7, Sunday is 1), yday (day of the year, 1–366), and isdst (daylight
     * saving flag, a boolean). This last field may be absent if the information
     * is not available.
     *
     * If format is not "*t", then date returns the date as a string, formatted
     * according to the same rules as the ISO C function strftime.
     *
     * When called without arguments, date returns a reasonable date and time
     * representation that depends on the host system and on the current locale.
     * (More specifically, os.date() is equivalent to os.date("%c").)
     *
     * On non-POSIX systems, this function may be not thread safe because of its
     * reliance on C function gmtime and C function localtime.
     */
    function date(format: '*t', time?: number): LuaDateInfoResult;

    /**
     * Returns the difference, in seconds, from time t1 to time t2 (where the
     * times are values returned by os.time). In POSIX, Windows, and some other
     * systems, this value is exactly t2-t1.
     */
    function difftime(t1: number, t2: number): number;

    /**
     * Returns the value of the process environment variable varname, or nil if
     * the variable is not defined.
     */
    function getenv(varname: string): string | undefined;

    /**
     * Deletes the file (or empty directory, on POSIX systems) with the given
     * name. If this function fails, it returns nil, plus a string describing the
     * error and the error code. Otherwise, it returns true.
     */
    function remove(filename: string): LuaMultiReturn<[true] | [undefined, string]>;

    /**
     * Renames the file or directory named oldname to newname. If this function
     * fails, it returns nil, plus a string describing the error and the error
     * code. Otherwise, it returns true.
     */
    function rename(oldname: string, newname: string): LuaMultiReturn<[true] | [undefined, string]>;

    /**
     * Sets the current locale of the program. locale is a system-dependent string
     * specifying a locale; category is an optional string describing which
     * category to change: "all", "collate", "ctype", "monetary", "numeric", or
     * "time"; the default category is "all". The function returns the name of the
     * new locale, or nil if the request cannot be honored.
     *
     * If locale is the empty string, the current locale is set to an
     * implementation-defined native locale. If locale is the string "C", the
     * current locale is set to the standard C locale.
     *
     * When called with nil as the first argument, this function only returns the
     * name of the current locale for the given category.
     *
     * This function may be not thread safe because of its reliance on C function
     * setlocale.
     */
    function setlocale(
        locale?: string,
        category?: 'all' | 'collate' | 'ctype' | 'monetary' | 'numeric' | 'time'
    ): string | undefined;

    /**
     * Returns the current time when called without arguments, or a time
     * representing the local date and time specified by the given table. This
     * table must have fields year, month, and day, and may have fields hour
     * (default is 12), min (default is 0), sec (default is 0), and isdst (default
     * is nil). Other fields are ignored. For a description of these fields, see
     * the os.date function.
     *
     * The values in these fields do not need to be inside their valid ranges. For
     * instance, if sec is -10, it means -10 seconds from the time specified by
     * the other fields; if hour is 1000, it means +1000 hours from the time
     * specified by the other fields.
     *
     * The returned value is a number, whose meaning depends on your system. In
     * POSIX, Windows, and some other systems, this number counts the number of
     * seconds since some given start time (the "epoch"). In other systems, the
     * meaning is not specified, and the number returned by time can be used only
     * as an argument to os.date and os.difftime.
     */
    function time(): number;

    /**
     * Returns the current time when called without arguments, or a time
     * representing the local date and time specified by the given table. This
     * table must have fields year, month, and day, and may have fields hour
     * (default is 12), min (default is 0), sec (default is 0), and isdst (default
     * is nil). Other fields are ignored. For a description of these fields, see
     * the os.date function.
     *
     * The values in these fields do not need to be inside their valid ranges. For
     * instance, if sec is -10, it means -10 seconds from the time specified by
     * the other fields; if hour is 1000, it means +1000 hours from the time
     * specified by the other fields.
     *
     * The returned value is a number, whose meaning depends on your system. In
     * POSIX, Windows, and some other systems, this number counts the number of
     * seconds since some given start time (the "epoch"). In other systems, the
     * meaning is not specified, and the number returned by time can be used only
     * as an argument to os.date and os.difftime.
     */
    function time(table: LuaDateInfo): number;

    /**
     * Returns a string with a file name that can be used for a temporary file.
     * The file must be explicitly opened before its use and explicitly removed
     * when no longer needed.
     *
     * On POSIX systems, this function also creates a file with that name, to
     * avoid security risks. (Someone else might create the file with wrong
     * permissions in the time between getting the name and creating the file.)
     * You still have to open the file to use it and to remove it (even if you do
     * not use it).
     *
     * When possible, you may prefer to use io.tmpfile, which automatically
     * removes the file when the program ends.
     */
    function tmpname(): string;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.4

/** @noSelfInFile */

/**
 * This library provides generic functions for string manipulation, such as
 * finding and extracting substrings, and pattern matching. When indexing a
 * string in Lua, the first character is at position 1 (not at 0, as in C).
 * Indices are allowed to be negative and are interpreted as indexing backwards,
 * from the end of the string. Thus, the last character is at position -1, and
 * so on.
 *
 * The string library provides all its functions inside the table string. It
 * also sets a metatable for strings where the __index field points to the
 * string table. Therefore, you can use the string functions in object-oriented
 * style. For instance, string.byte(s,i) can be written as s:byte(i).
 *
 * The string library assumes one-byte character encodings.
 */
declare namespace string {
    /**
     * Returns the internal numeric codes of the characters s[i], s[i+1], ...,
     * s[j]. The default value for i is 1; the default value for j is i. These
     * indices are corrected following the same rules of function string.sub.
     *
     * Numeric codes are not necessarily portable across platforms.
     */
    function byte(s: string, i?: number): number;
    function byte(s: string, i?: number, j?: number): LuaMultiReturn<number[]>;

    /**
     * Receives zero or more integers. Returns a string with length equal to the
     * number of arguments, in which each character has the internal numeric code
     * equal to its corresponding argument.
     *
     * Numeric codes are not necessarily portable across platforms.
     */
    function char(...args: number[]): string;

    /**
     * Returns a string containing a binary representation of the given function,
     * so that a later load on this string returns a copy of the function (but
     * with new upvalues).
     */
    function dump(func: Function): string;

    /**
     * Looks for the first match of pattern (see §6.4.1) in the string s. If it
     * finds a match, then find returns the indices of s where this occurrence
     * starts and ends; otherwise, it returns nil. A third, optional numeric
     * argument init specifies where to start the search; its default value is 1
     * and can be negative. A value of true as a fourth, optional argument plain
     * turns off the pattern matching facilities, so the function does a plain
     * "find substring" operation, with no characters in pattern being considered
     * magic. Note that if plain is given, then init must be given as well.
     *
     * If the pattern has captures, then in a successful match the captured values
     * are also returned, after the two indices.
     */
    function find(
        s: string,
        pattern: string,
        init?: number,
        plain?: boolean
    ): LuaMultiReturn<[number, number, ...string[]] | []>;

    /**
     * Returns a formatted version of its variable number of arguments following
     * the description given in its first argument (which must be a string). The
     * format string follows the same rules as the ISO C function sprintf. The
     * only differences are that the options/modifiers *, h, L, l, n, and p are
     * not supported and that there is an extra option, q.
     *
     * The q option formats a string between double quotes, using escape sequences
     * when necessary to ensure that it can safely be read back by the Lua
     * interpreter. For instance, the call
     *
     * `string.format('%q', 'a string with "quotes" and \n new line')`
     *
     * may produce the string:
     *
     * `"a string with \"quotes\" and \
     *  new line"` Options A, a, E, e, f, G, and g all expect a number as
     * argument. Options c, d, i, o, u, X, and x expect an integer. When Lua is
     * compiled with a C89 compiler, options A and a (hexadecimal floats) do not
     * support any modifier (flags, width, length).
     *
     * Option s expects a string; if its argument is not a string, it is converted
     * to one following the same rules of tostring. If the option has any modifier
     * (flags, width, length), the string argument should not contain embedded
     * zeros.
     */
    function format(formatstring: string, ...args: any[]): string;

    /**
     * Returns an iterator function that, each time it is called, returns the next
     * captures from pattern (see §6.4.1) over the string s. If pattern specifies
     * no captures, then the whole match is produced in each call.
     *
     * As an example, the following loop will iterate over all the words from
     * string s, printing one per line:
     *
     * ```
     * s = "hello world from Lua"
     * for w in string.gmatch(s, "%a+") do
     *   print(w)
     * end
     * ```
     *
     * The next example collects all pairs key=value from the given string into a
     * table:
     *
     * ```
     * t = {}
     * s = "from=world, to=Lua"
     * for k, v in string.gmatch(s, "(%w+)=(%w+)") do
     *   t[k] = v
     * end
     * ```
     *
     * For this function, a caret '^' at the start of a pattern does not work as
     * an anchor, as this would prevent the iteration.
     */
    function gmatch(s: string, pattern: string): LuaIterable<LuaMultiReturn<string[]>>;

    /**
     * Returns a copy of s in which all (or the first n, if given) occurrences of
     * the pattern (see §6.4.1) have been replaced by a replacement string
     * specified by repl, which can be a string, a table, or a function. gsub also
     * returns, as its second value, the total number of matches that occurred.
     * The name gsub comes from Global SUBstitution.
     *
     * If repl is a string, then its value is used for replacement. The character
     * % works as an escape character: any sequence in repl of the form %d, with d
     * between 1 and 9, stands for the value of the d-th captured substring. The
     * sequence %0 stands for the whole match. The sequence %% stands for a single
     * %.
     *
     * If repl is a table, then the table is queried for every match, using the
     * first capture as the key.
     *
     * If repl is a function, then this function is called every time a match
     * occurs, with all captured substrings passed as arguments, in order.
     *
     * In any case, if the pattern specifies no captures, then it behaves as if
     * the whole pattern was inside a capture.
     *
     * If the value returned by the table query or by the function call is a
     * string or a number, then it is used as the replacement string; otherwise,
     * if it is false or nil, then there is no replacement (that is, the original
     * match is kept in the string).
     */
    function gsub(
        s: string,
        pattern: string,
        repl: string | Record<string, string> | ((...matches: string[]) => string),
        n?: number
    ): LuaMultiReturn<[string, number]>;

    /**
     * Receives a string and returns its length. The empty string "" has length 0.
     * Embedded zeros are counted, so "a\000bc\000" has length 5.
     */
    function len(s: string): number;

    /**
     * Receives a string and returns a copy of this string with all uppercase
     * letters changed to lowercase. All other characters are left unchanged. The
     * definition of what an uppercase letter is depends on the current locale.
     */
    function lower(s: string): string;

    /**
     * Looks for the first match of pattern (see §6.4.1) in the string s. If it
     * finds one, then match returns the captures from the pattern; otherwise it
     * returns nil. If pattern specifies no captures, then the whole match is
     * returned. A third, optional numeric argument init specifies where to start
     * the search; its default value is 1 and can be negative.
     */
    function match(s: string, pattern: string, init?: number): LuaMultiReturn<string[]>;

    /**
     * Returns a string that is the concatenation of `n` copies of the string `s`.
     */
    function rep(s: string, n: number): string;

    /**
     * Returns a string that is the string s reversed.
     */
    function reverse(s: string): string;

    /**
     * Returns the substring of s that starts at i and continues until j; i and j
     * can be negative. If j is absent, then it is assumed to be equal to -1
     * (which is the same as the string length). In particular, the call
     * string.sub(s,1,j) returns a prefix of s with length j, and string.sub(s,
     * -i) (for a positive i) returns a suffix of s with length i.
     *
     * If, after the translation of negative indices, i is less than 1, it is
     * corrected to 1. If j is greater than the string length, it is corrected to
     * that length. If, after these corrections, i is greater than j, the function
     * returns the empty string.
     */
    function sub(s: string, i: number, j?: number): string;

    /**
     * Receives a string and returns a copy of this string with all lowercase
     * letters changed to uppercase. All other characters are left unchanged. The
     * definition of what a lowercase letter is depends on the current locale.
     */
    function upper(s: string): string;
}

// Based on https://www.lua.org/manual/5.3/manual.html#6.6

/** @noSelfInFile */

/**
 * This library provides generic functions for table manipulation. It provides
 * all its functions inside the table table.
 *
 * Remember that, whenever an operation needs the length of a table, all caveats
 * about the length operator apply (see §3.4.7). All functions ignore
 * non-numeric keys in the tables given as arguments.
 */
declare namespace table {
    /**
     * Given a list where all elements are strings or numbers, returns the string
     * list[i]..sep..list[i+1] ··· sep..list[j]. The default value for sep is the
     * empty string, the default for i is 1, and the default for j is #list. If i
     * is greater than j, returns the empty string.
     */
    function concat(list: (string | number)[], sep?: string, i?: number, j?: number): string;

    /**
     * Inserts element value at position pos in list, shifting up the elements
     * list[pos], list[pos+1], ···, list[#list]. The default value for pos is
     * #list+1, so that a call table.insert(t,x) inserts x at the end of list t.
     */
    function insert<T>(list: T[], value: T): void;
    function insert<T>(list: T[], pos: number, value: T): void;

    /**
     * Removes from list the element at position pos, returning the value of the
     * removed element. When pos is an integer between 1 and #list, it shifts down
     * the elements list[pos+1], list[pos+2], ···, list[#list] and erases element
     * list[#list]; The index pos can also be 0 when #list is 0, or #list + 1; in
     * those cases, the function erases the element list[pos].
     *
     * The default value for pos is #list, so that a call table.remove(l) removes
     * the last element of list l.
     */
    function remove<T>(list: T[], pos?: number): T | undefined;

    /**
     * Sorts list elements in a given order, in-place, from list[1] to
     * list[#list]. If comp is given, then it must be a function that receives two
     * list elements and returns true when the first element must come before the
     * second in the final order (so that, after the sort, i < j implies not
     * comp(list[j],list[i])). If comp is not given, then the standard Lua
     * operator < is used instead.
     *
     * Note that the comp function must define a strict partial order over the
     * elements in the list; that is, it must be asymmetric and transitive.
     * Otherwise, no valid sort may be possible.
     *
     * The sort algorithm is not stable: elements considered equal by the given
     * order may have their relative positions changed by the sort.
     */
    function sort<T>(list: T[], comp?: (a: T, b: T) => boolean): void;
}


/** @noSelfInFile */

/**
 * This function is a generic interface to the garbage collector. It performs
 * different functions according to its first argument, opt.
 *
 * Returns the total memory in use by Lua in Kbytes. The value has a fractional
 * part, so that it multiplied by 1024 gives the exact number of bytes in use by
 * Lua (except for overflows).
 */
declare function collectgarbage(opt: 'count'): number;

/**
 * Returns the elements from the given list. This function is equivalent to
 *
 * `return list[i], list[i+1], ···, list[j]`
 *
 * except that the above code can be written only for a fixed number of
 * elements. By default, i is 1 and j is the length of the list, as defined by
 * the length operator (see §2.5.5).
 */
declare function unpack<T extends any[]>(list: T): LuaMultiReturn<T>;
declare function unpack<T>(list: T[], i: number, j?: number): LuaMultiReturn<T[]>;

/**
 * Returns the current environment in use by the function. f can be a Lua
 * function or a number that specifies the function at that stack level: Level 1
 * is the function calling getfenv. If the given function is not a Lua function,
 * or if f is 0, getfenv returns the global environment. The default for f is 1.
 */
declare function getfenv(f?: Function | number): any;

/**
 * Sets the environment to be used by the given function. f can be a Lua
 * function or a number that specifies the function at that stack level: Level 1
 * is the function calling setfenv. setfenv returns the given function.
 *
 * As a special case, when f is 0 setfenv changes the environment of the running
 * thread. In this case, setfenv returns no values.
 */
declare function setfenv<T extends Function>(f: T, table: object): T;
declare function setfenv(f: 0, table: object): Function;
declare function setfenv(f: number, table: object): void;

declare namespace debug {
    /**
     * Returns the environment of object o.
     */
    function getfenv(o: object): any;

    /**
     * Sets the environment of the given object to the given table. Returns
     * object.
     */
    function setfenv<T extends object>(o: T, table: object): T;
}

declare namespace package {
    /**
     * A table used by require to control how to load modules.
     *
     * Each entry in this table is a searcher function. When looking for a module,
     * require calls each of these searchers in ascending order, with the module
     * name (the argument given to require) as its sole parameter. The function
     * can return another function (the module loader) plus an extra value that
     * will be passed to that loader, or a string explaining why it did not find
     * that module (or nil if it has nothing to say).
     *
     * Lua initializes this table with four searcher functions.
     *
     * The first searcher simply looks for a loader in the package.preload table.
     *
     * The second searcher looks for a loader as a Lua library, using the path
     * stored at package.path. The search is done as described in function
     * package.searchpath.
     *
     * The third searcher looks for a loader as a C library, using the path given
     * by the variable package.cpath. Again, the search is done as described in
     * function package.searchpath. For instance, if the C path is the string
     *
     * `./?.so;./?.dll;/usr/local/?/init.so`
     *
     * the searcher for module foo will try to open the files ./foo.so, ./foo.dll,
     * and /usr/local/foo/init.so, in that order. Once it finds a C library, this
     * searcher first uses a dynamic link facility to link the application with
     * the library. Then it tries to find a C function inside the library to be
     * used as the loader. The name of this C function is the string "luaopen_"
     * concatenated with a copy of the module name where each dot is replaced by
     * an underscore. Moreover, if the module name has a hyphen, its suffix after
     * (and including) the first hyphen is removed. For instance, if the module
     * name is a.b.c-v2.1, the function name will be luaopen_a_b_c.
     *
     * The fourth searcher tries an all-in-one loader. It searches the C path for
     * a library for the root name of the given module. For instance, when
     * requiring a.b.c, it will search for a C library for a. If found, it looks
     * into it for an open function for the submodule; in our example, that would
     * be luaopen_a_b_c. With this facility, a package can pack several C
     * submodules into one single library, with each submodule keeping its
     * original open function.
     */
    var loaders: (
        | ((modname: string) => LuaMultiReturn<[(modname: string) => void]>)
        | (<T>(modname: string) => LuaMultiReturn<[(modname: string, extra: T) => T, T]>)
        | string
    )[];
}

declare namespace os {
    /**
     * This function is equivalent to the C function system. It passes command to
     * be executed by an operating system shell. It returns a status code, which
     * is system-dependent. If command is absent, then it returns nonzero if a
     * shell is available and zero otherwise.
     */
    function execute(command?: string): number;
}

declare namespace math {
    /**
     * Returns the base-10 logarithm of x.
     */
    function log10(x: number): number;
}

declare namespace table {
    /**
     * Returns the largest positive numerical index of the given table, or zero if
     * the table has no positive numerical indices. (To do its job this function
     * does a linear traversal of the whole table.)
     */
    function maxn(table: object): number;
}

declare namespace coroutine {
    /**
     * Returns the running coroutine, or nil when called by the main thread.
     */
    function running(): LuaThread | undefined;
}

declare namespace io {
    type FileReadNumberFormat = '*n';
    type FileReadLineFormat = '*l';
    type FileReadFormat = FileReadNumberFormat | FileReadLineFormat | '*a' | '*L' | number;
}

/** @noSelfInFile */

/**
 * Loads a chunk.
 *
 * If chunk is a string, the chunk is this string. If chunk is a function, load
 * calls it repeatedly to get the chunk pieces. Each call to chunk must return a
 * string that concatenates with previous results. A return of an empty string,
 * nil, or no value signals the end of the chunk.
 *
 * If there are no syntactic errors, returns the compiled chunk as a function;
 * otherwise, returns nil plus the error message.
 *
 * If the resulting function has upvalues, the first upvalue is set to the value
 * of env, if that parameter is given, or to the value of the global
 * environment. Other upvalues are initialized with nil. (When you load a main
 * chunk, the resulting function will always have exactly one upvalue, the _ENV
 * variable (see §2.2). However, when you load a binary chunk created from a
 * function (see string.dump), the resulting function can have an arbitrary
 * number of upvalues.) All upvalues are fresh, that is, they are not shared
 * with any other function.
 *
 * chunkname is used as the name of the chunk for error messages and debug
 * information (see §4.9). When absent, it defaults to chunk, if chunk is a
 * string, or to "=(load)" otherwise.
 *
 * The string mode controls whether the chunk can be text or binary (that is, a
 * precompiled chunk). It may be the string "b" (only binary chunks), "t" (only
 * text chunks), or "bt" (both binary and text). The default is "bt".
 *
 * Lua does not check the consistency of binary chunks. Maliciously crafted
 * binary chunks can crash the interpreter.
 */
declare function load(
    chunk: string | (() => string | null | undefined),
    chunkname?: string,
    mode?: 'b' | 't' | 'bt',
    env?: object
): LuaMultiReturn<[() => any] | [undefined, string]>;

/**
 * Similar to load, but gets the chunk from file filename or from the standard
 * input, if no file name is given.
 */
declare function loadfile(
    filename?: string,
    mode?: 'b' | 't' | 'bt',
    env?: object
): LuaMultiReturn<[() => any] | [undefined, string]>;

/**
 * This function is similar to pcall, except that it sets a new message handler
 * msgh.
 */
declare function xpcall<This, Args extends any[], R, E>(
    f: (this: This, ...args: Args) => R,
    msgh: (this: void, err: any) => E,
    context: This,
    ...args: Args
): LuaMultiReturn<[true, R] | [false, E]>;

declare function xpcall<Args extends any[], R, E>(
    f: (this: void, ...args: Args) => R,
    msgh: (err: any) => E,
    ...args: Args
): LuaMultiReturn<[true, R] | [false, E]>;

declare namespace debug {
    interface FunctionInfo<T extends Function = Function> {
        nparams: number;
        isvararg: boolean;
    }

    /**
     * This function returns the name and the value of the local variable with
     * index local of the function at level f of the stack. This function accesses
     * not only explicit local variables, but also parameters, temporaries, etc.
     *
     * The first parameter or local variable has index 1, and so on, following the
     * order that they are declared in the code, counting only the variables that
     * are active in the current scope of the function. Negative indices refer to
     * vararg parameters; -1 is the first vararg parameter. The function returns
     * nil if there is no variable with the given index, and raises an error when
     * called with a level out of range. (You can call debug.getinfo to check
     * whether the level is valid.)
     *
     * Variable names starting with '(' (open parenthesis) represent variables
     * with no known names (internal variables such as loop control variables, and
     * variables from chunks saved without debug information).
     *
     * The parameter f may also be a function. In that case, getlocal returns only
     * the name of function parameters.
     */
    function getlocal(f: Function | number, local: number): LuaMultiReturn<[string, any]>;
    function getlocal(
        thread: LuaThread,
        f: Function | number,
        local: number
    ): LuaMultiReturn<[string, any]>;

    /**
     * Returns a unique identifier (as a light userdata) for the upvalue numbered
     * n from the given function.
     *
     * These unique identifiers allow a program to check whether different
     * closures share upvalues. Lua closures that share an upvalue (that is, that
     * access a same external local variable) will return identical ids for those
     * upvalue indices.
     */
    function upvalueid(f: Function, n: number): LuaUserdata;

    /**
     * Make the n1-th upvalue of the Lua closure f1 refer to the n2-th upvalue of
     * the Lua closure f2.
     */
    function upvaluejoin(f1: Function, n1: number, f2: Function, n2: number): void;
}

declare namespace math {
    /**
     * Returns the logarithm of x in the given base. The default for base is e (so
     * that the function returns the natural logarithm of x).
     */
    function log(x: number, base?: number): number;
}

declare namespace string {
    /**
     * Returns a string that is the concatenation of n copies of the string s
     * separated by the string sep. The default value for sep is the empty string
     * (that is, no separator). Returns the empty string if n is not positive.
     *
     * (Note that it is very easy to exhaust the memory of your machine with a
     * single call to this function.)
     */
    function rep(s: string, n: number, sep?: string): string;
}

declare namespace os {
    /**
     * Calls the ISO C function exit to terminate the host program. If code is
     * true, the returned status is EXIT_SUCCESS; if code is false, the returned
     * status is EXIT_FAILURE; if code is a number, the returned status is this
     * number. The default value for code is true.
     *
     * If the optional second argument close is true, closes the Lua state before
     * exiting.
     */
    function exit(code?: boolean | number, close?: boolean): never;
}

/** @noSelfInFile */

declare namespace math {
    /**
     * Returns the arc tangent of x (in radians).
     */
    function atan(x: number): number;

    /**
     * Returns the arc tangent of y/x (in radians), but uses the signs of both
     * parameters to find the quadrant of the result. (It also handles correctly
     * the case of x being zero.)
     */
    function atan2(y: number, x: number): number;

    /**
     * Returns the hyperbolic cosine of x.
     */
    function cosh(x: number): number;

    /**
     * Returns m and e such that x = m2e, e is an integer and the absolute value
     * of m is in the range [0.5, 1) (or zero when x is zero).
     */
    function frexp(x: number): number;

    /**
     * Returns m2e (e should be an integer).
     */
    function ldexp(m: number, e: number): number;

    /**
     * Returns xy. (You can also use the expression x^y to compute this value.)
     */
    function pow(x: number, y: number): number;

    /**
     * Returns the hyperbolic sine of x.
     */
    function sinh(x: number): number;

    /**
     * Returns the hyperbolic tangent of x.
     */
    function tanh(x: number): number;
}

/** @noSelfInFile */

declare namespace math {
    /**
     * When called without arguments, returns a pseudo-random float with uniform
     * distribution in the range [0,1). When called with two integers m and n,
     * math.random returns a pseudo-random integer with uniform distribution in
     * the range [m, n]. (The value n-m cannot be negative and must fit in a Lua
     * integer.) The call math.random(n) is equivalent to math.random(1,n).
     *
     * This function is an interface to the underling pseudo-random generator
     * function provided by C.
     */
    function random(m?: number, n?: number): number;

    /**
     * Sets x as the "seed" for the pseudo-random generator: equal seeds produce
     * equal sequences of numbers.
     */
    function randomseed(x: number): number;
}

/** @noSelfInFile */

/**
 * Loads a chunk.
 *
 * If chunk is a string, the chunk is this string. If chunk is a function, load
 * calls it repeatedly to get the chunk pieces. Each call to chunk must return a
 * string that concatenates with previous results. A return of an empty string,
 * nil, or no value signals the end of the chunk.
 *
 * If there are no syntactic errors, returns the compiled chunk as a function;
 * otherwise, returns nil plus the error message.
 *
 * If the resulting function has upvalues, the first upvalue is set to the value
 * of env, if that parameter is given, or to the value of the global
 * environment. Other upvalues are initialized with nil. (When you load a main
 * chunk, the resulting function will always have exactly one upvalue, the _ENV
 * variable (see §2.2). However, when you load a binary chunk created from a
 * function (see string.dump), the resulting function can have an arbitrary
 * number of upvalues.) All upvalues are fresh, that is, they are not shared
 * with any other function.
 *
 * chunkname is used as the name of the chunk for error messages and debug
 * information (see §4.9). When absent, it defaults to chunk, if chunk is a
 * string, or to "=(load)" otherwise.
 *
 * The string mode controls whether the chunk can be text or binary (that is, a
 * precompiled chunk). It may be the string "b" (only binary chunks), "t" (only
 * text chunks), or "bt" (both binary and text). The default is "bt".
 *
 * Lua does not check the consistency of binary chunks. Maliciously crafted
 * binary chunks can crash the interpreter.
 */
declare function loadstring(
    chunk: string | (() => string | null | undefined),
    chunkname?: string,
    mode?: 'b' | 't' | 'bt',
    env?: object
): LuaMultiReturn<[() => any] | [undefined, string]>;

declare namespace bit {
    /**
     * Normalizes a number to the numeric range for bit operations and returns it.
     * This function is usually not needed since all bit operations already
     * normalize all of their input arguments. Check the operational semantics for
     * details.
     */
    function tobit(x: number): number;

    /**
     * Converts its first argument to a hex string. The number of hex digits is
     * given by the absolute value of the optional second argument. Positive
     * numbers between 1 and 8 generate lowercase hex digits. Negative numbers
     * generate uppercase hex digits. Only the least-significant 4*|n| bits are
     * used. The default is to generate 8 lowercase hex digits.
     */
    function tohex(x: number, n?: number): string;

    /**
     * Returns the bitwise not of its argument.
     */
    function bnot(x: number): number;

    /**
     * Returns the bitwise or of all of its arguments.
     */
    function bor(x: number, ...rest: number[]): number;
    /**
     * Returns the bitwise and of all of its arguments.
     */
    function band(x: number, ...rest: number[]): number;
    /**
     * Returns the bitwise xor of all of its arguments.
     */
    function bxor(x: number, ...rest: number[]): number;

    /**
     * Returns the (bitwise logical left-shift, bitwise logical right-shift,
     * bitwise arithmetic right-shift) of its first argument by the number of bits
     * given by the second argument.
     *
     * Logical shifts treat the first argument as an unsigned number and shift in
     * 0-bits. Arithmetic right-shift treats the most-significant bit as a sign
     * bit and replicates it.
     *
     * Only the lower 5 bits of the shift count are used (reduces to the range
     * [0..31]).
     */
    function lshift(x: number, n: number): number;
    /**
     * Returns the (bitwise logical left-shift, bitwise logical right-shift,
     * bitwise arithmetic right-shift) of its first argument by the number of bits
     * given by the second argument.
     *
     * Logical shifts treat the first argument as an unsigned number and shift in
     * 0-bits. Arithmetic right-shift treats the most-significant bit as a sign
     * bit and replicates it.
     *
     * Only the lower 5 bits of the shift count are used (reduces to the range
     * [0..31]).
     */
    function rshift(x: number, n: number): number;
    /**
     * Returns the (bitwise logical left-shift, bitwise logical right-shift,
     * bitwise arithmetic right-shift) of its first argument by the number of bits
     * given by the second argument.
     *
     * Logical shifts treat the first argument as an unsigned number and shift in
     * 0-bits. Arithmetic right-shift treats the most-significant bit as a sign
     * bit and replicates it.
     *
     * Only the lower 5 bits of the shift count are used (reduces to the range
     * [0..31]).
     */
    function arshift(x: number, n: number): number;

    /**
     * Returns bitwise left rotation of its first argument by the number of bits
     * given by the second argument. Bits shifted out on one side are shifted back
     * in on the other side.
     *
     * Only the lower 5 bits of the rotate count are used (reduces to the range
     * [0..31]).
     */
    function rol(x: number, n: number): number;

    /**
     * Returns bitwise right rotation of its first argument by the number of bits
     * given by the second argument. Bits shifted out on one side are shifted back
     * in on the other side.
     *
     * Only the lower 5 bits of the rotate count are used (reduces to the range
     * [0..31]).
     */
    function ror(x: number, n: number): number;

    /**
     * Swaps the bytes of its argument and returns it. This can be used to convert
     * little-endian 32 bit numbers to big-endian 32 bit numbers or vice versa
     */
    function bswap(x: number): number;
}

declare namespace ffi {
    /**
     * Adds multiple C declarations for types or external symbols (named variables
     * or functions). def must be a Lua string. It's recommended to use the
     * syntactic sugar for string arguments as follows:
     *
     * The contents of the string must be a sequence of C declarations, separated
     * by semicolons. The trailing semicolon for a single declaration may be
     * omitted.
     *
     * Please note that external symbols are only declared, but they are not bound
     * to any specific address, yet. Binding is achieved with C library namespaces
     * (see below).
     *
     * C declarations are not passed through a C pre-processor, yet. No
     * pre-processor tokens are allowed, except for #pragma pack. Replace #define
     * in existing C header files with enum, static const or typedef and/or pass
     * the files through an external C pre-processor (once). Be careful not to
     * include unneeded or redundant declarations from unrelated header files.
     */
    function cdef(defs: string): void;

    /**
     * This is the default C library namespace — note the uppercase 'C'. It binds
     * to the default set of symbols or libraries on the target system. These are
     * more or less the same as a C compiler would offer by default, without
     * specifying extra link libraries.
     *
     * On POSIX systems, this binds to symbols in the default or global namespace.
     * This includes all exported symbols from the executable and any libraries
     * loaded into the global namespace. This includes at least libc, libm, libdl
     * (on Linux), libgcc (if compiled with GCC), as well as any exported symbols
     * from the Lua/C API provided by LuaJIT itself.
     *
     * On Windows systems, this binds to symbols exported from the *.exe, the
     * lua51.dll (i.e. the Lua/C API provided by LuaJIT itself), the C runtime
     * library LuaJIT was linked with (msvcrt*.dll), kernel32.dll, user32.dll and
     * gdi32.dll.
     */
    const C: Record<string, any>;

    /**
     * This loads the dynamic library given by name and returns a new C library
     * namespace which binds to its symbols. On POSIX systems, if global is true,
     * the library symbols are loaded into the global namespace, too.
     *
     * If name is a path, the library is loaded from this path. Otherwise name is
     * canonicalized in a system-dependent way and searched in the default search
     * path for dynamic libraries:
     *
     * On POSIX systems, if the name contains no dot, the extension .so is
     * appended. Also, the lib prefix is prepended if necessary. So ffi.load("z")
     * looks for "libz.so" in the default shared library search path.
     *
     * On Windows systems, if the name contains no dot, the extension .dll is
     * appended. So ffi.load("ws2_32") looks for "ws2_32.dll" in the default DLL
     * search path.
     */
    function load(name: string, global?: boolean): any;

    // TODO: http://luajit.org/ext_ffi_api.html
}

declare namespace jit {
    /**
     * Turns the whole JIT compiler on (default).
     *
     * This function is typically used with the command line option -j on.
     */
    function on(): void;

    /**
     * Turns the whole JIT compiler off.
     *
     * This function is typically used with the command line option -j off.
     */
    function off(): void;

    /**
     * Flushes the whole cache of compiled code.
     */
    function flush(): void;

    /**
     * Enables JIT compilation for a Lua function (this is the default).
     *
     * The current function, i.e. the Lua function calling this library function,
     * can also be specified by passing true as the first argument.
     *
     * If the second argument is true, JIT compilation is also enabled, disabled
     * or flushed recursively for all sub-functions of a function. With false only
     * the sub-functions are affected.
     *
     * This function only sets a flag which is checked when the function is about
     * to be compiled. It does not trigger immediate compilation.
     */
    function on(func: Function | true, recursive?: boolean): void;

    /**
     * Disables JIT compilation for a Lua function and flushes any already
     * compiled code from the code cache.
     *
     * The current function, i.e. the Lua function calling this library function,
     * can also be specified by passing true as the first argument.
     *
     * If the second argument is true, JIT compilation is also enabled, disabled
     * or flushed recursively for all sub-functions of a function. With false only
     * the sub-functions are affected.
     *
     * This function only sets a flag which is checked when the function is about
     * to be compiled. It does not trigger immediate compilation.
     */
    function off(func: Function | true, recursive?: boolean): void;

    /**
     * Flushes the code, but doesn't affect the enable/disable status.
     *
     * The current function, i.e. the Lua function calling this library function,
     * can also be specified by passing true as the first argument.
     *
     * If the second argument is true, JIT compilation is also enabled, disabled
     * or flushed recursively for all sub-functions of a function. With false only
     * the sub-functions are affected.
     */
    function flush(func: Function | true, recursive?: boolean): void;

    /**
     * Flushes the root trace, specified by its number, and all of its side traces
     * from the cache. The code for the trace will be retained as long as there
     * are any other traces which link to it.
     */
    function flush(tr: number): void;

    /**
     * Returns the current status of the JIT compiler. The first result is either
     * true or false if the JIT compiler is turned on or off. The remaining
     * results are strings for CPU-specific features and enabled optimizations.
     */
    function status(): LuaMultiReturn<[boolean, ...string[]]>;

    /**
     * Contains the LuaJIT version string.
     */
    const version: string;

    /**
     * Contains the version number of the LuaJIT core. Version xx.yy.zz is
     * represented by the decimal number xxyyzz.
     */
    const version_num: number;

    /**
     * Contains the target OS name.
     */
    const os: 'Windows' | 'Linux' | 'OSX' | 'BSD' | 'POSIX' | 'Other';

    /**
     * Contains the target architecture name.
     */
    const arch: 'x86' | 'x64' | 'arm' | 'ppc' | 'ppcspe' | 'mips';

    /**
     * This sub-module provides the backend for the -O command line option.
     *
     * You can also use it programmatically, e.g.:
     *
     * ```
     * jit.opt.start(2) -- same as -O2
     * jit.opt.start("-dce")
     * jit.opt.start("hotloop=10", "hotexit=2")
     * ```
     *
     * Unlike in LuaJIT 1.x, the module is built-in and optimization is turned on
     * by default! It's no longer necessary to run require("jit.opt").start(),
     * which was one of the ways to enable optimization.
     */
    const opt: any;

    /**
     * This sub-module holds functions to introspect the bytecode, generated
     * traces, the IR and the generated machine code. The functionality provided
     * by this module is still in flux and therefore undocumented.
     *
     * The debug modules -jbc, -jv and -jdump make extensive use of these
     * functions. Please check out their source code, if you want to know more.
     */
    const util: any;
}



