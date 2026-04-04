---
title: Class Styles
description: Configure how TypeScript classes are emitted to Lua.
---

TypeScript classes can be emitted using different Lua object system conventions. By default, tslua emits TSTL-compatible prototype chains. The `classStyle` option lets you target alternative object systems like [luabind](https://luabind.sourceforge.net/) or [middleclass](https://github.com/kikito/middleclass).

## Quick start

Add `classStyle` to your tsconfig.json under the `tstl` key:

```json
{
  "tstl": {
    "luaTarget": "JIT",
    "classStyle": {
      "declare": "call-chain",
      "constructorName": "__init",
      "new": "direct-call",
      "super": "base-direct",
      "prototype": false
    }
  }
}
```

This configuration targets luabind-style classes. The same TypeScript:

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

Produces different Lua depending on the class style:

### Default (TSTL)

```lua
local Greeter = __TS__Class()
Greeter.name = "Greeter"
function Greeter.prototype.____constructor(self, name)
    self.name = name
end
function Greeter.prototype.speak(self)
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

### Luabind style

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

### Middleclass style

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

## Options reference

### `declare`

How class declarations are emitted.

| Value            | Output                                            | Use case                     |
| ---------------- | ------------------------------------------------- | ---------------------------- |
| `"tstl"`         | `__TS__Class()` + `__TS__ClassExtends(cls, base)` | TSTL compatibility (default) |
| `"call-chain"`   | `class("Name")(base)`                             | luabind (C++ binding)        |
| `"call-extends"` | `class("Name", base)`                             | middleclass, 30log           |

### `constructorName`

The method name used for constructors. Any string is accepted.

| Value               | Output                 |
| ------------------- | ---------------------- |
| `"____constructor"` | TSTL default           |
| `"__init"`          | luabind convention     |
| `"initialize"`      | middleclass convention |

Any string is accepted.

### `new`

How `new Class(args)` is emitted.

| Value           | Output                   | Use case                       |
| --------------- | ------------------------ | ------------------------------ |
| `"tstl-new"`    | `__TS__New(Class, args)` | TSTL compatibility (default)   |
| `"direct-call"` | `Class(args)`            | luabind (classes are callable) |
| `"method-new"`  | `Class:new(args)`        | middleclass                    |

### `super`

How `super()` and `super.method()` calls are emitted.

| Value           | Output                                       | Use case                     |
| --------------- | -------------------------------------------- | ---------------------------- |
| `"tstl"`        | `Base.prototype.____constructor(self, args)` | TSTL compatibility (default) |
| `"base-direct"` | `Base.__init(self, args)`                    | luabind                      |
| `"class-super"` | `Class.super.initialize(self, args)`         | middleclass                  |

### `instanceOf`

How `instanceof` checks are emitted.

| Value      | Output                         | Use case                     |
| ---------- | ------------------------------ | ---------------------------- |
| `"tstl"`   | `__TS__InstanceOf(obj, Class)` | TSTL compatibility (default) |
| `"method"` | `obj:isInstanceOf(Class)`      | middleclass                  |
| `"none"`   | Emits a diagnostic error       | luabind (no runtime support) |

### `prototype`

Whether methods are attached to `Class.prototype` or directly to `Class`.

| Value   | Output                                  |
| ------- | --------------------------------------- |
| `true`  | `Class.prototype.method = fn` (default) |
| `false` | `Class.method = fn`                     |

Set to `false` for luabind and middleclass, which don't use a separate prototype table.

### `staticMembers`

Whether static class members are allowed.

| Value     | Behavior                                                  |
| --------- | --------------------------------------------------------- |
| `"allow"` | Emit normally (default)                                   |
| `"error"` | Emit a diagnostic error (luabind doesn't support statics) |

## Recipes

### Luabind (X-Ray Engine, other C++ bindings)

```json
{
  "classStyle": {
    "declare": "call-chain",
    "constructorName": "__init",
    "new": "direct-call",
    "super": "base-direct",
    "instanceOf": "none",
    "prototype": false,
    "staticMembers": "error"
  }
}
```

### Middleclass (LOVE, general Lua)

```json
{
  "classStyle": {
    "declare": "call-extends",
    "constructorName": "initialize",
    "new": "method-new",
    "super": "class-super",
    "instanceOf": "method",
    "prototype": false
  }
}
```
