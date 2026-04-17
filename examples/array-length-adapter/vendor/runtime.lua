-- Stand-in implementation for the host-provided Len primitive. In a real
-- embedded deployment this would be provided by the host (e.g. a C binding
-- to the host container's true length). Here we just shell out to rawlen
-- with a distinctive marker so it's easy to see in output.

function Len(arr)
    -- PRINT-MARKER: if you see this message at runtime, Len was called.
    return rawlen(arr)
end
