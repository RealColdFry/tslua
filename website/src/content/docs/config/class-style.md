---
title: Class Styles
description: Configure how TypeScript classes are emitted to Lua.
---

TypeScript classes can be emitted using different Lua object system conventions. By default, tslua emits TSTL-compatible prototype chains. The `classStyle` option lets you target alternative object systems.

Available styles: [`tstl`](#tstl-default) (default), [`luabind`](#luabind), [`middleclass`](#middleclass), [`inline`](#inline).

## Quick start

Set `classStyle` in your tsconfig.json under the `tstl` key:

```json
{
  "tstl": {
    "luaTarget": "JIT",
    "classStyle": "luabind"
  }
}
```

The examples below all use this TypeScript:

```typescript
class Animal {
  name: string;
  constructor(name: string) {
    this.name = name;
  }
  speak(): string {
    return this.name + " makes a sound";
  }
}

class Dog extends Animal {
  constructor(name: string) {
    super(name);
  }
  speak(): string {
    return this.name + " barks";
  }
}

const d = new Dog("Rex");
```

## `"tstl"` (default)

TSTL-compatible prototype chains. Requires lualib (`__TS__Class`, `__TS__New`, etc.).

- **`new`**: `__TS__New(C, args)`
- **`super()`**: `Base.prototype.____constructor(self, args)`
- **`instanceof`**: `__TS__InstanceOf(obj, C)`
- Methods on `C.prototype`

```lua
local Animal = __TS__Class()
Animal.name = "Animal"
function Animal.prototype.____constructor(self, name)
    self.name = name
end
function Animal.prototype.speak(self)
    return self.name .. " makes a sound"
end

local Dog = __TS__Class()
Dog.name = "Dog"
__TS__ClassExtends(Dog, Animal)
function Dog.prototype.____constructor(self, name)
    Animal.prototype.____constructor(self, name)
end
function Dog.prototype.speak(self)
    return self.name .. " barks"
end

local d = __TS__New(Dog, "Rex")
```

## `"luabind"`

For [luabind](https://luabind.sourceforge.net/) and other C++ binding systems. Requires a `class` global.

- **`new`**: `C(args)`
- **`super()`**: `Base.__init(self, args)`
- **`instanceof`**: not supported
- Static members: not supported

```lua
local Animal = class("Animal")()
Animal.__name = "Animal"
function Animal.__init(self, name)
    self.name = name
end
function Animal.speak(self)
    return self.name .. " makes a sound"
end

local Dog = class("Dog")(Animal)
Dog.__name = "Dog"
function Dog.__init(self, name)
    Animal.__init(self, name)
end
function Dog.speak(self)
    return self.name .. " barks"
end

local d = Dog("Rex")
```

## `"middleclass"`

For [middleclass](https://github.com/kikito/middleclass) and similar libraries. Requires a `class` global.

- **`new`**: `C:new(args)`
- **`super()`**: `C.super.initialize(self, args)`
- **`instanceof`**: `obj:isInstanceOf(C)`
- Methods on `C` directly

```lua
local Animal = class("Animal")
Animal.__name = "Animal"
function Animal.initialize(self, name)
    self.name = name
end
function Animal.speak(self)
    return self.name .. " makes a sound"
end

local Dog = class("Dog", Animal)
Dog.__name = "Dog"
function Dog.initialize(self, name)
    Dog.super.initialize(self, name)
end
function Dog.speak(self)
    return self.name .. " barks"
end

local d = Dog:new("Rex")
```

## `"inline"`

No runtime library needed. Classes use inline `setmetatable` and `__index` chains, scoped in `do` blocks.

- **`new`**: `C.new(args)`
- **`super()`**: `super.constructor(self, args)`
- **`instanceof`**: not supported
- Methods on `C` directly

```lua
local Animal
do
    Animal = setmetatable({}, {
        __tostring = function()
            return "Animal"
        end,
    })
    Animal.__index = Animal
    function Animal.new(...)
        local self = setmetatable({}, Animal)
        return self:constructor(...) or self
    end
    function Animal:constructor(name)
        self.name = name
    end
    function Animal:speak()
        return self.name .. " makes a sound"
    end
end

local Dog
do
    local super = Animal
    Dog = setmetatable({}, {
        __tostring = function()
            return "Dog"
        end,
        __index = super,
    })
    Dog.__index = Dog
    function Dog.new(...)
        local self = setmetatable({}, Dog)
        return self:constructor(...) or self
    end
    function Dog:constructor(name)
        super.constructor(self, name)
    end
    function Dog:speak()
        return self.name .. " barks"
    end
end

local d = Dog.new("Rex")
```
