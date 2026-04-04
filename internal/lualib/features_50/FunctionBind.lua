local function __TS__FunctionBind(fn, ...)
    local boundArgs = arg
    return function(____, ...)
        local args = arg
        __TS__ArrayUnshift(
            args,
            unpack(boundArgs)
        )
        return fn(unpack(args))
    end
end