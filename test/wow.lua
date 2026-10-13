-- Fakes the WoW API that the addon uses. Widgets keep their state. Timers fire on a game clock that only wow.Run
-- moves, one frame per call. The fake lays out only a frame with one anchor on UIParent. Text size is approximate: 6
-- units per character, 13 per line.
local wow = { now = 1790968020, timers = {}, sent = {}, chat = {}, loaded = {}, texts = {}, addons = {}, bits = {}, payload = nil }

local Widget = {}
Widget.__index = function(_, key)
    if Widget[key] then
        return Widget[key]
    elseif key:match("^%u") then
        return function() end
    end
end

local function widget(kind, parent)
    return setmetatable({ _kind = kind, _parent = parent, _shown = true, _enabled = true, _scripts = {}, _width = 400, _height = 400, _text = "" }, Widget)
end

function Widget:IsObjectType(kind) return self._kind == kind end
function Widget:GetObjectType() return self._kind end
function Widget:Show() self._shown = true if self._scripts.OnShow then self._scripts.OnShow(self) end end
function Widget:Hide() self._shown = false if self._scripts.OnHide then self._scripts.OnHide(self) end end
function Widget:SetShown(shown) if shown then self:Show() else self:Hide() end end
function Widget:IsShown() return self._shown end
function Widget:IsVisible()
    local node = self
    while node do
        if not node._shown then return false end
        node = node._parent
    end
    return true
end
function Widget:SetParent(parent) self._parent = parent end
function Widget:GetParent() return self._parent end
function Widget:SetScript(name, handler) self._scripts[name] = handler end
function Widget:HookScript(name, handler)
    local previous = self._scripts[name]
    self._scripts[name] = function(...) if previous then previous(...) end handler(...) end
end
function Widget:GetScript(name) return self._scripts[name] end
function Widget:Fire(name, ...) if self._scripts[name] then return self._scripts[name](self, ...) end end
function Widget:SetSize(width, height) self._width, self._height = width, height end
function Widget:SetWidth(width) self._width = width end
function Widget:SetHeight(height) self._height = height end
function Widget:GetWidth() return self._width end
function Widget:GetHeight() return self._height end
function Widget:GetSize() return self._width, self._height end
function Widget:GetEffectiveScale() return 1 end
function Widget:GetFrameLevel() return 1 end
local SPOT = {
    TOPLEFT = { 0, 1 }, TOP = { 0.5, 1 }, TOPRIGHT = { 1, 1 }, LEFT = { 0, 0.5 }, CENTER = { 0.5, 0.5 }, RIGHT = { 1, 0.5 },
    BOTTOMLEFT = { 0, 0 }, BOTTOM = { 0.5, 0 }, BOTTOMRIGHT = { 1, 0 },
}
function Widget:SetPoint(point, relative, relativePoint, x, y)
    if type(relative) == "number" then
        relative, relativePoint, x, y = nil, point, relative, relativePoint
    elseif type(relativePoint) == "number" then
        relativePoint, x, y = point, relativePoint, x
    end
    self._points = self._points or {}
    for index, anchor in ipairs(self._points) do
        if anchor.point == point then
            table.remove(self._points, index)
            break
        end
    end
    table.insert(self._points, { point = point, relative = relative or self._parent, relativePoint = relativePoint or point, x = x or 0, y = y or 0 })
end
function Widget:ClearAllPoints() self._points = {} end
function Widget:GetPoint(index)
    local anchor = self._points and self._points[index or 1]
    if not anchor then
        return "CENTER", nil, "CENTER", 0, 0
    end
    return anchor.point, anchor.relative, anchor.relativePoint, anchor.x, anchor.y
end
local function corner(self)
    local anchor = self._points and #self._points == 1 and self._points[1]
    if not anchor or anchor.relative ~= UIParent then
        return nil
    end
    local on, at = SPOT[anchor.point], SPOT[anchor.relativePoint]
    return at[1] * UIParent._width + anchor.x - on[1] * self._width, at[2] * UIParent._height + anchor.y - on[2] * self._height
end
function Widget:GetLeft() return (corner(self)) end
function Widget:GetBottom() return select(2, corner(self)) end
function Widget:GetRight() local left = corner(self) return left and left + self._width end
function Widget:GetTop() local _, bottom = corner(self) return bottom and bottom + self._height end
function Widget:GetCenter() return 0, 0 end
function Widget:IsMouseOver() return false end
function Widget:SetText(text) self._text = text == nil and "" or tostring(text) if self._scripts.OnTextChanged then self._scripts.OnTextChanged(self, true) end end
function Widget:GetText() return self._text end
function Widget:GetStringHeight() return 13 * math.max(1, math.ceil(#self._text * 6 / math.max(1, self._width))) end
function Widget:GetUnboundedStringWidth() return #self._text * 6 end
function Widget:Enable() self._enabled = true end
function Widget:Disable() self._enabled = false end
function Widget:SetEnabled(enabled) self._enabled = enabled and true or false end
function Widget:IsEnabled() return self._enabled end
function Widget:GetVerticalScrollRange() return 0 end
function Widget:SetVerticalScroll(offset) self._scroll = offset end
function Widget:GetVerticalScroll() return self._scroll or 0 end
function Widget:SetAlpha(alpha) self._alpha = alpha end
function Widget:CreateTexture() return widget("Texture", self) end
function Widget:CreateMaskTexture() return widget("MaskTexture", self) end
function Widget:CreateFontString() local text = widget("FontString", self) table.insert(wow.texts, text) return text end
function Widget:CreateAnimationGroup() return widget("AnimationGroup", self) end
function Widget:CreateAnimation() return widget("Animation", self) end
function Widget:Play() if self._scripts.OnPlay then self._scripts.OnPlay(self) end if self._scripts.OnFinished then self._scripts.OnFinished(self) end end
function Widget:Restart() self:Play() end
function Widget:RegisterEvent(event) self._events = self._events or {} self._events[event] = true end
function Widget:UnregisterEvent(event) if self._events then self._events[event] = nil end end
function Widget:UnregisterAllEvents() self._events = {} end

wow.frames = {}

function CreateFrame(kind, name, parent)
    local frame = widget(kind, parent)
    table.insert(wow.frames, frame)
    if name then
        _G[name] = frame
    end
    return frame
end

function CreateFont() return widget("Font") end

UIParent = widget("Frame")
UIParent._width, UIParent._height = 1366, 768
Minimap = widget("Frame", UIParent)
GameTooltip = widget("GameTooltip", UIParent)
DEFAULT_CHAT_FRAME = { AddMessage = function(_, text) table.insert(wow.chat, text) end }
ChatTypeInfo = { WHISPER = { r = 1, g = 0.5, b = 1 } }
NORMAL_FONT_COLOR = { GetRGB = function() return 1, 0.82, 0 end }
HIGHLIGHT_FONT_COLOR = { GetRGB = function() return 1, 1, 1 end }
SOUNDKIT = { TELL_MESSAGE = 3081 }
NUM_BAG_SLOTS = 4
UISpecialFrames = {}
SlashCmdList = {}
Enum = {
    CompressionMethod = { Zlib = 1 },
    SpellBookSpellBank = { Player = 0 },
    SpellBookItemType = { Spell = 1 },
}

EventRegistry = { callbacks = {} }
function EventRegistry:RegisterCallback(event, handler, owner) self.callbacks[event] = function(...) handler(owner, ...) end end

function PlaySound() end
function time() return math.floor(wow.now) end
function GetTime() return wow.now end
PixelUtil = { GetPixelToUIUnitFactor = function() return 768 / 1080 end }
function issecretvalue() return false end
function GetCursorPosition() return 0, 0 end
function ScrollingEdit_OnTextChanged() end
function ScrollingEdit_OnCursorChanged() end
function ScrollingEdit_OnUpdate() end
function IsShiftKeyDown() return false end
C_UI = { Reload = function() end }

function strtrim(text) return (tostring(text):gsub("^%s+", ""):gsub("%s+$", "")) end
function strsplit(delimiter, text, limit)
    local parts, start = {}, 1
    while true do
        if limit and #parts == limit - 1 then
            table.insert(parts, text:sub(start))
            break
        end
        local index = text:find(delimiter, start, true)
        if not index then
            table.insert(parts, text:sub(start))
            break
        end
        table.insert(parts, text:sub(start, index - 1))
        start = index + 1
    end
    return (table.unpack or unpack)(parts)
end
function wipe(t) for key in pairs(t) do t[key] = nil end return t end
function tContains(t, value) for _, item in ipairs(t) do if item == value then return true end end return false end
function CopyTable(source)
    local copy = {}
    for key, value in pairs(source) do copy[key] = type(value) == "table" and CopyTable(value) or value end
    return copy
end

C_Timer = {}
local function timer(seconds, callback, repeating)
    local handle = { due = wow.now + seconds, seconds = seconds, callback = callback, repeating = repeating }
    function handle:Cancel() self.cancelled = true end
    table.insert(wow.timers, handle)
    return handle
end
function C_Timer.NewTimer(seconds, callback) return timer(seconds, callback, false) end
function C_Timer.NewTicker(seconds, callback) return timer(seconds, callback, true) end
function C_Timer.After(seconds, callback) timer(seconds, callback, false) end

-- Moves game time by seconds. Each timer fires at its due time, in order. Then each visible frame gets one OnUpdate.
function wow.Run(seconds)
    local stop = wow.now + seconds
    while true do
        local soonest
        for index = #wow.timers, 1, -1 do
            local handle = wow.timers[index]
            if handle.cancelled then
                table.remove(wow.timers, index)
            elseif handle.due <= stop and (not soonest or handle.due <= soonest.due) then
                soonest = handle
            end
        end
        if not soonest then
            break
        end
        wow.now = math.max(wow.now, soonest.due)
        if soonest.repeating then
            soonest.due = soonest.due + soonest.seconds
        else
            soonest.cancelled = true
        end
        soonest.callback(soonest)
    end
    wow.now = stop
    for _, frame in ipairs(wow.frames) do
        if frame._scripts.OnUpdate and frame:IsVisible() then
            frame._scripts.OnUpdate(frame, seconds)
        end
    end
end

function wow.Event(event, ...)
    for _, frame in ipairs(wow.frames) do
        if frame._events and frame._events[event] then
            frame:Fire("OnEvent", event, ...)
        end
    end
end

C_AddOns = {
    GetAddOnMetadata = function() return "0.3.0" end,
    DoesAddOnExist = function() return true end,
    EnableAddOn = function(name) wow.addons[name] = true end,
    DisableAddOn = function(name) wow.addons[name] = nil end,
    -- Each save records the 128 thott-bot-chat-bit-NNN states as one 16-byte frame. bit-001 is the high bit of the first byte.
    SaveAddOns = function()
        local bytes = {}
        for byte = 0, 15 do
            local value = 0
            for bit = 1, 8 do
                value = value * 2 + (wow.addons[string.format("thott-bot-chat-bit-%03d", byte * 8 + bit)] and 1 or 0)
            end
            bytes[byte + 1] = string.char(value)
        end
        table.insert(wow.bits, table.concat(bytes))
    end,
    LoadAddOn = function(name)
        table.insert(wow.loaded, { name = name, at = wow.now })
        thott_bot_chat_receive("payload")
        return true
    end,
}

local function encode(value)
    if type(value) == "string" then
        return '"' .. value:gsub('[%c"\\]', function(c) return string.format("\\u%04x", c:byte()) end) .. '"'
    elseif type(value) ~= "table" then
        return tostring(value)
    end
    local parts = {}
    for index, item in ipairs(value) do
        parts[index] = encode(item)
    end
    if #parts > 0 then
        return "[" .. table.concat(parts, ",") .. "]"
    end
    for key, item in pairs(value) do
        table.insert(parts, encode(key) .. ":" .. encode(item))
    end
    return "{" .. table.concat(parts, ",") .. "}"
end

C_EncodingUtil = {
    SerializeJSON = function(packet) table.insert(wow.sent, packet) return encode(packet) end,
    CompressString = function(text) return text end,
    DeserializeJSON = function() return wow.payload end,
}

C_Map = { GetBestMapForUnit = function() return 1417 end, GetPlayerMapPosition = function() return { GetXY = function() return 0.45, 0.47 end } end }
C_QuestLog = {
    GetNumQuestLogEntries = function() return 1 end,
    GetInfo = function() return { questID = 676, title = "Foul Magics", level = 36, isHeader = false, isHidden = false } end,
    GetQuestObjectives = function() return { { text = "2/4 Syndicate Conjuror", numFulfilled = 2, numRequired = 4 } } end,
    IsComplete = function() return false end,
}
C_Container = { GetContainerNumSlots = function() return 2 end, GetContainerItemInfo = function(_, slot) return slot == 1 and { itemID = 8079, stackCount = 5 } or nil end }
C_SpecializationInfo = { GetActiveSpecGroup = function() return 1 end, GetCombatConfigIDForSpecGroup = function() return 7 end }
C_Traits = {
    GetConfigInfo = function() return { treeIDs = { 1 } } end,
    GetTreeNodes = function() return { 11 } end,
    GetNodeInfo = function() return { activeRank = 2, activeEntry = { entryID = 3 } } end,
    GetEntryInfo = function() return { definitionID = 4 } end,
    GetDefinitionInfo = function() return { spellID = 11069 } end,
}
C_SkillInfo = { GetNumSkillLines = function() return 1 end, GetSkillLineInfo = function() return { skillID = 129, name = "First Aid", rank = 150, maxRank = 225, isHeader = false } end }
C_SpellBook = {
    GetNumSpellBookSkillLines = function() return 1 end,
    GetSpellBookSkillLineInfo = function() return { itemIndexOffset = 0, numSpellBookItems = 1, shouldHide = false } end,
    GetSpellBookItemInfo = function() return { spellID = 133, itemType = 1, isOffSpec = false, name = "Fireball", subName = "Rank 1" } end,
}
C_Reputation = { GetNumFactions = function() return 1 end, GetFactionDataByIndex = function() return { factionID = 72, name = "Stormwind", currentStanding = 3000, isHeader = false } end }
C_UnitAuras = { GetAuraDataByIndex = function(_, index) return index == 1 and { spellId = 1459, name = "Arcane Intellect" } or nil end }
C_GameRules = { IsHardcoreActive = function() return false end }
C_Item = {
    GetItemInfo = function(id) if id == 6948 then return "Hearthstone", "|cffffffff|Hitem:6948::::::::33:::::::::|h[Hearthstone]|h|r" end end,
    RequestLoadItemDataByID = function(id) wow.requested = id end,
}
C_Spell = {
    GetSpellName = function(id) return id == 6117 and "Mage Armor" or nil end,
    GetSpellLink = function(id) return id == 6117 and "|cff71d5ff|Hspell:6117:0|h[Mage Armor]|h|r" or nil end,
    RequestLoadSpellData = function() end,
}

function UnitRace() return "Human", "Human", 1 end
function UnitClass() return "Mage", "MAGE", 8 end
function UnitLevel() return 33 end
function UnitFactionGroup() return "Alliance" end
function UnitXP() return 41200 end
function UnitXPMax() return 66300 end
function GetXPExhaustion() return 12000 end
function GetMoney() return 345678 end
function GetZoneText() return "Arathi Highlands" end
function GetSubZoneText() return "Refuge Pointe" end
function GetInstanceInfo() return "Eastern Kingdoms", "none", 0, "", 5, 0, false, 0 end
function IsResting() return true end
function UnitExists() return true end
function UnitGUID() return "Creature-0-1-0-1-2601-0" end
function UnitName() return "Elandra" end
function UnitClassification() return "normal" end
function UnitReaction() return 2 end
function GetInventoryItemID(_, slot) return slot == 16 and 15275 or nil end
function GetBuildInfo() return "1.60.1", "70170", "Oct 1 2026", 16001 end

local function decode(text)
    local index = 1
    local function space() index = text:find("[^%s]", index) or #text + 1 end
    local value
    local function literal(word, result) index = index + #word return result end
    local function str()
        local out, i = {}, index + 1
        while true do
            local c = text:sub(i, i)
            if c == '"' then break end
            if c == "\\" then
                local n = text:sub(i + 1, i + 1)
                if n == "u" then
                    local code = tonumber(text:sub(i + 2, i + 5), 16)
                    table.insert(out, code < 0x80 and string.char(code) or code < 0x800 and string.char(0xC0 + math.floor(code / 64), 0x80 + code % 64)
                        or string.char(0xE0 + math.floor(code / 4096), 0x80 + math.floor(code / 64) % 64, 0x80 + code % 64))
                    i = i + 6
                else
                    table.insert(out, ({ n = "\n", t = "\t", r = "\r", b = "\b", f = "\f" })[n] or n)
                    i = i + 2
                end
            else
                table.insert(out, c)
                i = i + 1
            end
        end
        index = i + 1
        return table.concat(out)
    end
    function value()
        space()
        local c = text:sub(index, index)
        if c == "{" then
            local object = {}
            index = index + 1
            space()
            if text:sub(index, index) == "}" then index = index + 1 return object end
            while true do
                space()
                local key = str()
                space()
                index = index + 1
                object[key] = value()
                space()
                local sep = text:sub(index, index)
                index = index + 1
                if sep == "}" then return object end
            end
        elseif c == "[" then
            local array = {}
            index = index + 1
            space()
            if text:sub(index, index) == "]" then index = index + 1 return array end
            while true do
                table.insert(array, value())
                space()
                local sep = text:sub(index, index)
                index = index + 1
                if sep == "]" then return array end
            end
        elseif c == '"' then
            return str()
        elseif text:sub(index, index + 3) == "true" then
            return literal("true", true)
        elseif text:sub(index, index + 4) == "false" then
            return literal("false", false)
        elseif text:sub(index, index + 3) == "null" then
            return literal("null", nil)
        end
        local number = text:match("^-?%d+%.?%d*[eE]?[-+]?%d*", index)
        index = index + #number
        return tonumber(number)
    end
    return value()
end
wow.Decode = decode

function wow.Samples(path)
    local file = assert(io.open(path))
    local samples = decode(file:read("*a"))
    file:close()
    return samples
end

function wow.Load(directory)
    local T = {}
    local toc = assert(io.open(directory .. "/thott-bot-chat.toc"))
    for line in toc:lines() do
        if line:match("%.lua$") then
            assert(loadfile(directory .. "/" .. line))("thott-bot-chat", T)
        end
    end
    toc:close()
    wow.Event("ADDON_LOADED", "thott-bot-chat")
    wow.Event("PLAYER_LOGIN")
    return T
end

return wow
