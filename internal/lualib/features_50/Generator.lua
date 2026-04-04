local function generatorIterator(self)
    return self
end
local function generatorNext(self, ...)
    local co = self.____coroutine
    if coroutine.status(co) == "dead" then
        return {done = true}
    end
    local status, value = coroutine.resume(
        co,
        unpack(arg)
    )
    if not status then
        error(value, 0)
    end
    return {
        value = value,
        done = coroutine.status(co) == "dead"
    }
end
local function __TS__Generator(fn)
    return function(...)
        local args = arg
        local argsLength = __TS__CountVarargs(unpack(arg))
        return {
            ____coroutine = coroutine.create(function() return fn(__TS__Unpack(args, 1, argsLength)) end),
            [Symbol.iterator] = generatorIterator,
            next = generatorNext
        }
    end
end