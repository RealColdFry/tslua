local function __TS__New(target, ...)
    local instance = setmetatable({}, target.prototype)
    instance:____constructor(unpack(arg))
    return instance
end