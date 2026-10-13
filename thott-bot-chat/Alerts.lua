local _, T = ...

local Kit, Format, Store = T.Kit, T.Format, T.Store
local Alerts = {}
T.Alerts = Alerts

local TOAST = { width = 280, seconds = 9, stack = 3 }
local toasts, button = {}, nil

local function toast()
    local frame = Kit.Panel(CreateFrame("Button", nil, UIParent), "surface-1")
    frame:SetFrameStrata("DIALOG")
    frame:SetSize(TOAST.width, 2 * Kit.space["space-3"] + 2 * Kit.space["space-4"])
    frame.stripe = Kit.Stripe(frame, "agent-needs")
    frame.title = Kit.Text(frame, "t-ui-strong", "ink")
    frame.title:SetPoint("TOPLEFT", Kit.space["space-4"], -Kit.space["space-3"])
    frame.title:SetPoint("RIGHT", -(Kit.space["space-3"] + Kit.space["space-4"]), 0)
    frame.title:SetHeight(Kit.space["space-4"])
    frame.title:SetJustifyV("MIDDLE")
    frame.title:SetWordWrap(false)
    frame.line = Kit.Text(frame, "t-meta", "ink-3")
    frame.line:SetPoint("TOPLEFT", frame.title, "BOTTOMLEFT")
    frame.line:SetHeight(Kit.space["space-4"])
    frame.line:SetJustifyV("MIDDLE")
    frame.close = Kit.Glyph(frame, "close")
    frame.close:SetPoint("TOPRIGHT", -Kit.space["space-3"], -Kit.space["space-3"] - 2)
    frame:SetScript("OnEnter", function(self) self.close:SetInk("ink") end)
    frame:SetScript("OnLeave", function(self) self.close:SetInk("ink-3") end)
    frame:SetScript("OnClick", function(self)
        Kit.Leave(self)
        T.Window.Show(self.id)
    end)
    Kit.Tip(frame, frame.title)
    frame:Hide()
    return frame
end

local function stack()
    local anchor = Minimap
    for _, frame in ipairs(toasts) do
        if frame:IsShown() then
            frame:ClearAllPoints()
            frame:SetPoint("TOPRIGHT", anchor, "BOTTOMRIGHT", 0, -Kit.space["space-2"])
            anchor = frame
        end
    end
end

local function show(thread, line)
    local frame
    for _, candidate in ipairs(toasts) do
        if candidate:IsShown() and candidate.id == thread.id then
            frame = candidate
            break
        elseif not frame and not candidate:IsShown() then
            frame = candidate
        end
    end
    if not frame then
        frame = #toasts < TOAST.stack and toast() or table.remove(toasts, 1)
        table.insert(toasts, frame)
    end
    frame.id = thread.id
    frame.title:SetText(Format.Escape(Store.Title(thread)))
    frame.line:SetText(line)
    frame.stripe:SetShown(Store.Rank(thread) == 0)
    Kit.Paint(frame.stripe, thread.state == "error" and "agent-error" or "agent-needs")
    if frame.timer then
        frame.timer:Cancel()
    end
    frame.timer = C_Timer.NewTimer(TOAST.seconds, function() Kit.Leave(frame) end)
    Kit.Rise(frame)
    stack()
end

local function chat(thread, words)
    local whisper = ChatTypeInfo.WHISPER
    local link = "|Haddon:thott-bot-chat:open:" .. thread.id .. "|h[Open]|h"
    DEFAULT_CHAT_FRAME:AddMessage(string.format("%s: %s %s", words, Format.Escape(Store.Title(thread)), link), whisper.r, whisper.g, whisper.b)
end

local function alert(thread, line, words)
    if T.Window.IsShown() and T.Window.Current() == thread.id then
        return
    end
    show(thread, line)
    if T.settings.alertChat then
        chat(thread, words)
    end
    if T.settings.alertSound then
        PlaySound(SOUNDKIT.TELL_MESSAGE)
    end
end

T.On("StoreChanged", function(change)
    Alerts.Refresh()
    for index, thread in ipairs(change.needs or {}) do
        if not change.initial or index == 1 then
            alert(thread, Format.Status(thread, time()), Format.Agent(thread) .. " needs you")
        end
    end
    for _, thread in ipairs(change.replied or {}) do
        alert(thread, "Replied", Format.Agent(thread) .. " replied")
    end
end)

EventRegistry:RegisterCallback("SetItemRef", function(_, link)
    local kind, addon, action, id = strsplit(":", link, 4)
    if kind == "addon" and addon == "thott-bot-chat" and action == "open" then
        T.Window.Show(id)
    end
end, Alerts)

function Alerts.Tooltip(owner)
    GameTooltip:SetOwner(owner, "ANCHOR_LEFT")
    GameTooltip:SetText("thott.bot chat")
    local waiting, working = Store.Waiting(), 0
    for _, thread in ipairs(Store.Threads()) do
        if Store.Rank(thread) == 1 then
            working = working + 1
        end
    end
    if waiting > 0 then
        GameTooltip:AddLine(waiting .. " need you", NORMAL_FONT_COLOR:GetRGB())
    end
    if working > 0 then
        GameTooltip:AddLine(working .. " working", HIGHLIGHT_FONT_COLOR:GetRGB())
    end
    GameTooltip:Show()
end

local function place()
    local angle = math.rad(T.settings.minimapAngle)
    local radius = Minimap:GetWidth() / 2 + Kit.space["space-2"]
    button:ClearAllPoints()
    button:SetPoint("CENTER", Minimap, "CENTER", math.cos(angle) * radius, math.sin(angle) * radius)
end

local function build()
    button = CreateFrame("Button", "thott_bot_chat_minimap_button", Minimap)
    button:SetSize(Kit.size["control-lg"], Kit.size["control-lg"])
    button:SetFrameLevel(Minimap:GetFrameLevel() + 8)
    button:RegisterForDrag("LeftButton")
    local mask = button:CreateMaskTexture()
    mask:SetTexture("Interface\\CharacterFrame\\TempPortraitAlphaMask", "CLAMPTOBLACKADDITIVE", "CLAMPTOBLACKADDITIVE")
    mask:SetSize(Kit.space["space-6"], Kit.space["space-6"])
    mask:SetPoint("CENTER")
    local ground = button:CreateTexture(nil, "BACKGROUND")
    ground:SetAllPoints(mask)
    Kit.Paint(ground, "well")
    ground:AddMaskTexture(mask)
    local icon = button:CreateTexture(nil, "ARTWORK")
    icon:SetTexture(T.media .. "Emblem")
    icon:SetSize(Kit.space["space-6"] - Kit.space["space-1"], Kit.space["space-6"] - Kit.space["space-1"])
    icon:SetPoint("CENTER")
    local ring = button:CreateTexture(nil, "OVERLAY")
    ring:SetTexture("Interface\\Minimap\\MiniMap-TrackingBorder")
    ring:SetSize(54, 54)
    ring:SetPoint("TOPLEFT")
    button:SetHighlightTexture("Interface\\Minimap\\UI-Minimap-ZoomButton-Highlight", "ADD")
    button.badge = CreateFrame("Frame", nil, button)
    button.badge:SetSize(Kit.space["space-4"], Kit.space["space-4"])
    button.badge:SetPoint("TOPRIGHT", Kit.space["space-1"], 0)
    Kit.Fill(button.badge, "agent-needs")
    Kit.Edge(button.badge)
    button.badge.text = Kit.Text(button.badge, "t-num", "bg")
    button.badge.text:SetPoint("CENTER")
    button:SetScript("OnClick", T.Window.Toggle)
    button:SetScript("OnEnter", Alerts.Tooltip)
    button:SetScript("OnLeave", function() GameTooltip:Hide() end)
    button:SetScript("OnDragStart", function(self)
        self:SetScript("OnUpdate", function()
            local x, y = GetCursorPosition()
            local scale = Minimap:GetEffectiveScale()
            local cx, cy = Minimap:GetCenter()
            T.settings.minimapAngle = math.deg(math.atan2(y / scale - cy, x / scale - cx))
            place()
        end)
    end)
    button:SetScript("OnDragStop", function(self) self:SetScript("OnUpdate", nil) end)
    place()
end

function Alerts.Refresh()
    if not button then
        return
    end
    button:SetShown(T.settings.minimap)
    local waiting = Store.Waiting()
    button.badge:SetShown(waiting > 0)
    button.badge.text:SetText(waiting)
end

function Alerts.Init()
    build()
    Alerts.Refresh()
end
