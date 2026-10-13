local addonName, T = ...

T.version = C_AddOns.GetAddOnMetadata(addonName, "Version")
T.media = "Interface\\AddOns\\" .. addonName .. "\\Media\\"

local listeners = {}

function T.On(event, handler)
    listeners[event] = listeners[event] or {}
    table.insert(listeners[event], handler)
end

function T.Fire(event, ...)
    for _, handler in ipairs(listeners[event] or {}) do
        handler(...)
    end
end

local defaults = {
    scale = 1,
    opacity = 1,
    alertChat = true,
    alertSound = true,
    minimap = true,
    minimapAngle = 225,
    window = { sidebar = true },
    choice = {},
    list = { group = "project", sort = "recent" },
    extras = true,
    context = {
        character = false, location = false, quests = false, gear = false, bags = false, talents = false,
        skills = false, spells = false, target = false, reputation = false, buffs = false,
    },
}

local function merge(saved, fallback)
    for key, value in pairs(fallback) do
        if saved[key] == nil then
            saved[key] = type(value) == "table" and CopyTable(value) or value
        elseif type(value) == "table" and type(saved[key]) == "table" then
            merge(saved[key], value)
        end
    end
    return saved
end

local events = CreateFrame("Frame")
events:RegisterEvent("ADDON_LOADED")
events:RegisterEvent("PLAYER_LOGIN")
events:SetScript("OnEvent", function(self, event, name)
    if event == "ADDON_LOADED" and name == addonName then
        thott_bot_chat_settings = merge(thott_bot_chat_settings or {}, defaults)
        T.settings = thott_bot_chat_settings
    elseif event == "PLAYER_LOGIN" then
        self:UnregisterAllEvents()
        T.Window.Init()
        T.Alerts.Init()
        T.Sync.Start()
    end
end)

SLASH_THOTT_BOT_CHAT1 = "/thott.bot"
SLASH_THOTT_BOT_CHAT2 = "/tb"
SlashCmdList.THOTT_BOT_CHAT = function()
    T.Window.Toggle()
end

function thott_bot_chat_on_addon_compartment_click()
    T.Window.Toggle()
end

function thott_bot_chat_on_addon_compartment_enter(_, button)
    T.Alerts.Tooltip(button)
end

function thott_bot_chat_on_addon_compartment_leave()
    GameTooltip:Hide()
end
