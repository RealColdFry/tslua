-- Mock luabind runtime for testing tslua's class style emit.
-- Simulates C++ luabind's class() function in pure Lua.
--
-- API:
--   local Cls = class("Name")()        → create class (no base)
--   local Cls = class("Name")(Base)    → create class extending Base
--   local obj = Cls(args)              → instantiate (calls __init)
--   Base.__init(self, args)            → super constructor call
--   Base.method(self, args)            → super method call
--
-- The real luabind registers classes globally via C++. This mock
-- returns the class from class()() for local assignment.

function class(name)
    return function(base)
        local c = {}
        c.__name = name
        c.__index = c

        if base then
            setmetatable(c, {
                __index = base,
                __call = function(cls, ...)
                    local instance = setmetatable({}, cls)
                    if cls.__init then
                        cls.__init(instance, ...)
                    end
                    return instance
                end,
            })
        else
            setmetatable(c, {
                __call = function(cls, ...)
                    local instance = setmetatable({}, cls)
                    if cls.__init then
                        cls.__init(instance, ...)
                    end
                    return instance
                end,
            })
        end

        return c
    end
end
