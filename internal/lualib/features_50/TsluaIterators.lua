local function __TS__MapForOfStep(map, prev)
    local key
    if prev == nil then
        key = map.firstKey
    else
        key = map.nextKey[prev]
    end
    if key == nil then
        return nil
    end
    return key, map.items[key]
end
local function __TS__MapForOf(map)
    return __TS__MapForOfStep, map, nil
end

local function __TS__MapKeysForOfStep(map, prev)
    local key
    if prev == nil then
        key = map.firstKey
    else
        key = map.nextKey[prev]
    end
    if key == nil then
        return nil
    end
    return key
end
local function __TS__MapKeysForOf(map)
    return __TS__MapKeysForOfStep, map, nil
end

local function __TS__MapValuesForOfStep(map, prev)
    local key
    if prev == nil then
        key = map.firstKey
    else
        key = map.nextKey[prev]
    end
    if key == nil then
        return nil
    end
    return key, map.items[key]
end
local function __TS__MapValuesForOf(map)
    return __TS__MapValuesForOfStep, map, nil
end

local function __TS__SetForOfStep(set, prev)
    local key
    if prev == nil then
        key = set.firstKey
    else
        key = set.nextKey[prev]
    end
    if key == nil then
        return nil
    end
    return key
end
local function __TS__SetForOf(set)
    return __TS__SetForOfStep, set, nil
end