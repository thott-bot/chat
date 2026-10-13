local _, T = ...

local Transport = { session = "s" .. time() }
T.Transport = Transport

local VERSION = 4
local SLOTS = 128
local BITS = 128
local FRAME_DATA = BITS / 8 - 1
local FRAME_SECONDS = 0.04

local bits = {}
for index = 1, BITS do
    bits[index] = string.format("thott-bot-chat-bit-%03d", index)
end

local slot, seq, counter = 1, 0, time() % 128
local queue, shown = {}, {}
local ticker

local function show(frame)
    for byte = 1, #frame do
        local value = frame:byte(byte)
        for bit = 0, 7 do
            local index = (byte - 1) * 8 + bit + 1
            local on = math.floor(value / 2 ^ (7 - bit)) % 2 == 1
            if shown[index] ~= on then
                if on then
                    C_AddOns.EnableAddOn(bits[index])
                else
                    C_AddOns.DisableAddOn(bits[index])
                end
                shown[index] = on
            end
        end
    end
    C_AddOns.SaveAddOns()
end

local function step()
    local packet = queue[1]
    if not packet then
        ticker:Cancel()
        ticker = nil
        return
    end
    local head = (packet.next == 1 and 128 or 0) + counter
    counter = (counter + 1) % 128
    show(string.char(head) .. packet.frames[packet.next])
    packet.next = packet.next + 1
    if packet.next > #packet.frames then
        table.remove(queue, 1)
        T.Fire("PacketSent", packet.seq)
    end
end

local function frames(body)
    local stream = string.char(math.floor(#body / 256), #body % 256) .. body
    local list = {}
    for start = 1, #stream, FRAME_DATA do
        local chunk = stream:sub(start, start + FRAME_DATA - 1)
        table.insert(list, chunk .. string.rep("\0", FRAME_DATA - #chunk))
    end
    return list
end

local function encode(packet)
    return C_EncodingUtil.CompressString(C_EncodingUtil.SerializeJSON(packet), Enum.CompressionMethod.Zlib)
end

function Transport.Send(packet)
    if not C_AddOns.DoesAddOnExist(bits[BITS]) then
        return nil, "uninstalled"
    end
    seq = seq + 1
    packet.seq, packet.session, packet.at = seq, Transport.session, time()
    local ok, body = pcall(encode, packet)
    if not ok then
        return nil, "unencodable"
    elseif #body > 65535 then
        return nil, "oversized"
    end
    table.insert(queue, { seq = seq, next = 1, frames = frames(body) })
    ticker = ticker or C_Timer.NewTicker(FRAME_SECONDS, step)
    return seq
end

function Transport.Sending(packetSeq)
    for _, packet in ipairs(queue) do
        if packet.seq == packetSeq then
            return true
        end
    end
    return false
end

function Transport.SlotsLeft()
    return SLOTS - slot + 1
end

function Transport.Load()
    if slot > SLOTS then
        return false, "exhausted"
    end
    local name = string.format("thott-bot-chat-slot-%03d", slot)
    if not C_AddOns.DoesAddOnExist(name) then
        return false, "uninstalled"
    end
    C_AddOns.EnableAddOn(name)
    local loaded, reason = C_AddOns.LoadAddOn(name)
    if not loaded then
        return false, reason
    end
    slot = slot + 1
    return true
end

function thott_bot_chat_receive(json)
    local ok, data = pcall(C_EncodingUtil.DeserializeJSON, json)
    if ok and type(data) == "table" and data.v == VERSION then
        T.Store.Apply(data)
    else
        T.Fire("Problem", "unreadable")
    end
end
