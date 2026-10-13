local _, T = ...

local Context = {}
T.Context = Context

local REACTION = { "hostile", "hostile", "unfriendly", "neutral", "friendly", "friendly", "friendly", "friendly" }

local function plain(value)
    if issecretvalue(value) then
        return nil
    end
    return value
end

local function list(items, describe)
    local out = {}
    for _, item in ipairs(items) do
        table.insert(out, describe(item))
    end
    return table.concat(out, ", ")
end

local function money(copper)
    return string.format("%dg %ds %dc", math.floor(copper / 10000), math.floor(copper / 100) % 100, copper % 100)
end

local function character()
    local xp, max = plain(UnitXP("player")), plain(UnitXPMax("player"))
    local progress = xp and max and string.format(" · XP %s / %s, rested %s", tostring(xp), tostring(max), tostring(GetXPExhaustion() or 0)) or ""
    return {
        string.format("Character: level %d %s %s, %s%s · %s", UnitLevel("player"), UnitRace("player"), UnitClass("player"),
            UnitFactionGroup("player"), progress, money(GetMoney())),
    }
end

local function location()
    local map = C_Map.GetBestMapForUnit("player")
    local position = map and C_Map.GetPlayerMapPosition(map, "player")
    local zone, sub = GetZoneText(), GetSubZoneText()
    local _, kind, _, difficulty = GetInstanceInfo()
    local line = "Location: " .. ((sub ~= "" and sub ~= zone) and (sub .. ", " .. zone) or zone)
    if position then
        local x, y = position:GetXY()
        line = line .. string.format(" %.1f, %.1f", x * 100, y * 100)
    end
    if kind ~= "none" then
        line = line .. " · " .. kind .. (difficulty ~= "" and (", " .. difficulty) or "")
    end
    return { line .. (IsResting() and " · resting" or "") }
end

local function target()
    if not UnitExists("target") then
        return nil
    end
    local kind, _, _, _, _, npc = strsplit("-", plain(UnitGUID("target")) or "")
    local who = (kind == "Creature" or kind == "Vehicle") and (" (npc:" .. npc .. ")") or (kind == "Player" and " (player)" or "")
    local name, level, class = plain(UnitName("target")), plain(UnitLevel("target")), plain(UnitClassification("target"))
    if not name and who == "" then
        return nil
    end
    return {
        string.format("Target:%s%s%s%s, %s", name and " " .. name or "", who, level and " level " .. level or "",
            (class and class ~= "normal") and (" " .. class) or "", REACTION[plain(UnitReaction("target", "player")) or 4] or "neutral"),
    }
end

local function quests()
    local lines = {}
    for index = 1, C_QuestLog.GetNumQuestLogEntries() do
        local info = C_QuestLog.GetInfo(index)
        if info and not info.isHeader and not info.isHidden then
            local objectives = list(C_QuestLog.GetQuestObjectives(info.questID) or {}, function(objective)
                return string.format("%d/%d %s", objective.numFulfilled, objective.numRequired, ((objective.text or ""):gsub("^%d+/%d+%s*", "")))
            end)
            table.insert(lines, string.format("  %s (quest:%d) level %d%s%s", info.title, info.questID, info.level,
                C_QuestLog.IsComplete(info.questID) and " · ready to turn in" or "", objectives ~= "" and (" · " .. objectives) or ""))
        end
    end
    table.insert(lines, 1, string.format("Quests (%d):", #lines))
    return lines
end

local function gear()
    local equipped = {}
    for slot = 1, 19 do
        local id = GetInventoryItemID("player", slot)
        if id then
            table.insert(equipped, "item:" .. id)
        end
    end
    return { "Equipped: " .. table.concat(equipped, ", ") }
end

local function bags()
    local items, counts = {}, {}
    for bag = 0, NUM_BAG_SLOTS do
        for slot = 1, C_Container.GetContainerNumSlots(bag) do
            local info = C_Container.GetContainerItemInfo(bag, slot)
            if info then
                if not counts[info.itemID] then
                    table.insert(items, info.itemID)
                end
                counts[info.itemID] = (counts[info.itemID] or 0) + info.stackCount
            end
        end
    end
    return { "Bags: " .. list(items, function(id) return "item:" .. id .. (counts[id] > 1 and (" ×" .. counts[id]) or "") end) }
end

local function talents()
    local configID = C_SpecializationInfo.GetCombatConfigIDForSpecGroup(C_SpecializationInfo.GetActiveSpecGroup())
    local config = configID and C_Traits.GetConfigInfo(configID)
    local spells = {}
    for _, treeID in ipairs(config and config.treeIDs or {}) do
        for _, nodeID in ipairs(C_Traits.GetTreeNodes(treeID)) do
            local node = C_Traits.GetNodeInfo(configID, nodeID)
            if node.activeRank > 0 and node.activeEntry then
                local entry = C_Traits.GetEntryInfo(configID, node.activeEntry.entryID)
                local definition = entry.definitionID and C_Traits.GetDefinitionInfo(entry.definitionID)
                if definition and definition.spellID then
                    table.insert(spells, string.format("%s (Rank %d)", C_Spell.GetSpellName(definition.spellID) or ("spell " .. definition.spellID), node.activeRank))
                end
            end
        end
    end
    return { "Talents: " .. table.concat(spells, ", ") }
end

local function skills()
    local lines = {}
    for index = 1, C_SkillInfo.GetNumSkillLines() do
        local skill = C_SkillInfo.GetSkillLineInfo(index)
        if skill and not skill.isHeader then
            table.insert(lines, string.format("%s %d/%d", skill.name, skill.rank, skill.maxRank))
        end
    end
    return { "Skills: " .. table.concat(lines, ", ") }
end

local function spells()
    local names, ranks = {}, {}
    for line = 1, C_SpellBook.GetNumSpellBookSkillLines() do
        local info = C_SpellBook.GetSpellBookSkillLineInfo(line)
        if not info.shouldHide then
            for slot = info.itemIndexOffset + 1, info.itemIndexOffset + info.numSpellBookItems do
                local item = C_SpellBook.GetSpellBookItemInfo(slot, Enum.SpellBookSpellBank.Player)
                if item.spellID and item.itemType == Enum.SpellBookItemType.Spell and not item.isOffSpec then
                    local rank = tonumber((item.subName or ""):match("(%d+)")) or 0
                    if not ranks[item.name] then
                        table.insert(names, item.name)
                    end
                    if rank >= (ranks[item.name] or 0) then
                        ranks[item.name] = rank
                    end
                end
            end
        end
    end
    return { "Spells known: " .. list(names, function(name)
        return ranks[name] > 0 and string.format("%s (Rank %d)", name, ranks[name]) or name
    end) }
end

local function reputation()
    local factions = {}
    for index = 1, C_Reputation.GetNumFactions() do
        local faction = C_Reputation.GetFactionDataByIndex(index)
        if faction and (not faction.isHeader or faction.isHeaderWithRep) then
            table.insert(factions, string.format("%s (faction:%d) %d", faction.name, faction.factionID, faction.currentStanding))
        end
    end
    return { "Reputation: " .. table.concat(factions, ", ") }
end

local function buffs()
    local names = {}
    for index = 1, 40 do
        local aura = C_UnitAuras.GetAuraDataByIndex("player", index, "HELPFUL")
        if not aura then
            break
        end
        local name = plain(aura.name)
        if name then
            table.insert(names, name)
        end
    end
    return { "Buffs: " .. table.concat(names, ", ") }
end

Context.parts = {
    { key = "character", label = "Character", lines = character },
    { key = "location", label = "Location", lines = location },
    { key = "quests", label = "Quest log", lines = quests },
    { key = "gear", label = "Gear", lines = gear },
    { key = "bags", label = "Bags", lines = bags },
    { key = "talents", label = "Talents", lines = talents },
    { key = "skills", label = "Skills", lines = skills },
    { key = "spells", label = "Spells known", lines = spells },
    { key = "target", label = "Target", lines = target },
    { key = "reputation", label = "Reputation", lines = reputation },
    { key = "buffs", label = "Buffs", lines = buffs },
}

function Context.Text()
    if not T.settings.extras then
        return nil
    end
    local version, build = GetBuildInfo()
    local lines = { string.format("Game: WoW Forever %s.%s%s", version, build,
        C_GameRules.IsHardcoreActive() and " · hardcore" or "") }
    local header = #lines
    for _, part in ipairs(Context.parts) do
        if T.settings.context[part.key] then
            local ok, partLines = pcall(part.lines)
            for _, line in ipairs(ok and partLines or {}) do
                table.insert(lines, line)
            end
        end
    end
    return #lines > header and table.concat(lines, "\n") or nil
end

function Context.Summary()
    if not T.settings.extras then
        return "Sending only your message"
    end
    local enabled = T.settings.context
    local parts = {}
    if enabled.character then
        table.insert(parts, "Level " .. UnitLevel("player") .. " " .. UnitClass("player"))
    end
    if enabled.location then
        local sub = GetSubZoneText()
        table.insert(parts, sub ~= "" and sub or GetZoneText())
    end
    if #parts == 0 then
        for _, part in ipairs(Context.parts) do
            if enabled[part.key] then
                return part.label
            end
        end
        return "Not sending your character"
    end
    return table.concat(parts, " · ")
end
