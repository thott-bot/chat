local _, T = ...

local Format = {}
T.Format = Format

function Format.Escape(text)
    return (tostring(text or ""):gsub("|", "||"))
end

function Format.Agent(thread)
    return thread.agent == "claude" and "Claude" or "Codex"
end

function Format.Ago(seconds)
    seconds = math.max(0, math.floor(seconds))
    if seconds < 60 then
        return "Just now"
    elseif seconds < 3600 then
        return math.floor(seconds / 60) .. "m ago"
    elseif seconds < 86400 then
        return math.floor(seconds / 3600) .. "h ago"
    end
    return math.floor(seconds / 86400) .. "d ago"
end

function Format.Clock(seconds)
    seconds = math.max(0, math.floor(seconds))
    return string.format("%d:%02d", math.floor(seconds / 60), seconds % 60)
end

function Format.Status(thread, now)
    local state = thread.state
    if state == "needs_you" then
        if not thread.approval then
            return "Waiting for your answer"
        end
        return thread.approval.kind == "file" and "Wants to change files" or "Wants to run a command"
    elseif state == "error" then
        return "Stopped"
    elseif state == "active" then
        local activity = thread.activity and thread.activity ~= "" and thread.activity or "Working"
        return activity .. " · " .. Format.Clock(now - (thread.since or thread.updated))
    elseif state == "queued" then
        return "Starting…"
    end
    return Format.Ago(now - thread.updated)
end

local waiting, failed = {}, {}

local LOOKUP = {
    item = function(id)
        local name, link = C_Item.GetItemInfo(id)
        if not name and not failed["item:" .. id] then
            waiting["item:" .. id] = true
            C_Item.RequestLoadItemDataByID(id)
        end
        return name, link
    end,
    spell = function(id)
        local name = C_Spell.GetSpellName(id)
        if not name and not failed["spell:" .. id] then
            waiting["spell:" .. id] = true
            C_Spell.RequestLoadSpellData(id)
        end
        return name, name and C_Spell.GetSpellLink(id)
    end,
}

function Format.Loaded(key, success)
    local was = waiting[key]
    waiting[key] = nil
    if success == false then
        failed[key] = true
    end
    return was and success ~= false
end

local function linkify(text)
    return (text:gsub("%[([^%]]+)%]%((%a+):(%d+)%)", function(label, kind, id)
        local lookup = LOOKUP[kind]
        local name, link
        if lookup then
            name, link = lookup(tonumber(id))
        end
        return link and name:lower() == label:lower() and link or label
    end))
end

local function emphasise(text, colour)
    text = Format.Escape(text):gsub("%*%*([^*]+)%*%*", "|c" .. colour .. "%1|r")
    return linkify((text:gsub("`([^`]+)`", "|c" .. colour .. "%1|r")))
end

function Format.Blocks(source, colour)
    local blocks, fenced = {}, false
    for line in (source .. "\n"):gmatch("(.-)\r?\n") do
        if line:find("^```") then
            fenced = not fenced
        elseif fenced then
            table.insert(blocks, { kind = "code", text = "|c" .. colour .. Format.Escape(line) .. "|r" })
        elseif line:find("%S") then
            local item = line:match("^%s*[%-%*] (.*)$")
            table.insert(blocks, { kind = item and "item" or "paragraph", text = emphasise(item or line, colour) })
        end
    end
    return blocks
end
