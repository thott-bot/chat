local _, T = ...

local Sync = { at = 0 }
T.Sync = Sync

local CADENCE = { 2, 3, 5, 8, 13 }
local STALE = 30
local LOW = 16

local timer, step

local function schedule(delay)
    if timer then
        timer:Cancel()
    end
    timer = delay and C_Timer.NewTimer(delay, Sync.Now) or nil
end

function Sync.Now()
    schedule(nil)
    local loaded, reason = T.Transport.Load()
    if not loaded then
        T.Fire("Problem", reason)
        return
    end
    Sync.at = time()
    local left = T.Transport.SlotsLeft()
    if left <= LOW then
        T.Fire("Problem", left == 0 and "exhausted" or "low")
    end
    if left > 0 and T.Store.Awaiting() then
        step = math.min((step or 0) + 1, #CADENCE)
        schedule(CADENCE[step])
    else
        step = nil
    end
end

function Sync.Fresh()
    if time() - Sync.at >= STALE then
        Sync.Now()
    end
end

function Sync.Start()
    T.Transport.Send({ type = "hello", addon = T.version })
    Sync.Now()
end

T.On("PacketSent", function(seq)
    if T.Store.Tracked(seq) and T.Store.Awaiting() then
        step = 1
        schedule(CADENCE[1])
    end
end)
