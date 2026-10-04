; Minimal TOS stand-in for SNDH replay.
;
; It lives in the TOS ROM window and provides what SNDH drivers expect from
; the OS: a 200 Hz Timer C tick (with etv_timer), a VBL handler with the VBL
; queue, and GEMDOS/BIOS/XBIOS traps. Trap work is forwarded to the Go side
; through the host registers; everything that has to run guest code (replay
; calls, Supexec, the VBL queue) stays here.
;
; m68kasm v1.6.1 lays out labels as if a one-operand instruction (JMP, JSR,
; PEA, TAS, Scc, CLR, TST) with an absolute operand had no extension words, so
; this file reaches absolute addresses through (An) or a two-operand MOVE
; instead.
;
; Symbols supplied by the Go side (see machine.go):
;   TUNE        load address of the SNDH image
;   SSP_TOP     initial supervisor stack
;   USP_TOP     initial user stack
;   H_ARGS      host: trap argument pointer (long, write)
;   H_FRAME     host: trap exception frame pointer (long, write)
;   H_CALL      host: perform trap call, value = trap number (word, write)
;   H_RESULT    host: result of the last call (long, read)
;   H_EVENT     host: machine event (word, write)
;   H_SUBTUNE   host: subtune for INIT (word, read)
;   V_ACC       Timer C replay accumulator (word)
;   V_RATE      Timer C replay rate in Hz, 0 = not via Timer C (word)
;   V_BUSY      replay routine running (byte)
;   V_VBLPLAY   replay from the VBL handler (byte)
;   V_TICK4     Timer C /4 divider for etv_timer (word)
;   V_ISR       MFP in-service register of the replay timer (long)
;   V_ISRCLR    value that clears its in-service bit (byte)
;   EV_INIT     event: INIT returned
;   EV_CRASH    event: unexpected exception

MFP_ISRB    = $fffa11

etv_timer   = $400
_timr_ms    = $442
_vblsem     = $452
_nvbls      = $454
_vblqueue   = $456
_frclock    = $466
_vbclock    = $462
_hz_200     = $4ba

        .org    $fc0000

; OS header (TOS 1.04 layout)
os_entry:
        bra.s   os_start            ; +0
        dc.w    $0104               ; +2  os_version
        dc.l    reset               ; +4  reseth
        dc.l    os_entry            ; +8  os_beg
        dc.l    $00000800           ; +12 os_end
        dc.l    reset               ; +16 os_rsv1
        dc.l    0                   ; +20 os_magic
        dc.l    $04061989           ; +24 os_date
        dc.w    $0003               ; +28 os_conf (PAL)
        dc.w    $1286               ; +30 os_dosdate
        dc.l    0                   ; +32 p_root
        dc.l    0                   ; +36 pkbshift
        dc.l    0                   ; +40 p_run
        dc.l    0                   ; +44 p_rsv2
os_start:
        bra.w   reset

; Boot: call INIT with the subtune in d0, then idle with interrupts on.
reset:
        move.w  #$2700,sr
        lea     SSP_TOP,sp
        lea     USP_TOP,a0
        move.l  a0,usp
        move.w  #$2300,sr
        moveq   #0,d0
        move.w  H_SUBTUNE,d0
        lea     TUNE,a0
        jsr     (a0)
        move.w  #EV_INIT,H_EVENT    ; host arms the replay interrupt
        move.w  #$2300,sr
idle:
        stop    #$2300
        bra.s   idle

; Call the PLAY routine unless it is already running.
call_play:
        movem.l d0-d7/a0-a6,-(sp)
        lea     V_BUSY,a0
        tas     (a0)
        bne.s   1f
        lea     TUNE+8,a0
        jsr     (a0)
        lea     V_BUSY,a0
        clr.b   (a0)
1:      movem.l (sp)+,d0-d7/a0-a6
        rts

; MFP Timer C, 200 Hz: system tick, etv_timer every fourth tick and replay
; at V_RATE Hz through an accumulator.
timer_c:
        addq.l  #1,_hz_200
        movem.l d0-d7/a0-a6,-(sp)
        move.b  #$df,MFP_ISRB       ; end of interrupt, let Timer D through
        move.w  #$2500,sr
        subq.w  #1,V_TICK4
        bne.s   1f
        move.w  #4,V_TICK4
        move.w  _timr_ms,-(sp)
        movea.l etv_timer,a0
        jsr     (a0)
        addq.l  #2,sp
1:      move.w  V_RATE,d0
        beq.s   3f
        add.w   V_ACC,d0
        move.w  d0,V_ACC
        move.b  V_BUSY,d0
        bne.s   3f
2:      cmpi.w  #200,V_ACC
        blo.s   3f
        subi.w  #200,V_ACC
        bsr     call_play
        bra.s   2b
3:      movem.l (sp)+,d0-d7/a0-a6
        rte

; Replay through MFP Timer A, B or D, programmed by the host.
timer_play:
        move.l  a0,-(sp)
        movea.l V_ISR,a0
        move.b  V_ISRCLR,(a0)       ; end of interrupt
        movea.l (sp)+,a0
        move.w  #$2500,sr
        bsr     call_play
        rte

; Level 4 autovector: VBL, with the TOS VBL queue.
vbl:
        addq.l  #1,_frclock
        subq.w  #1,_vblsem
        bmi.s   9f
        movem.l d0-d7/a0-a6,-(sp)
        addq.l  #1,_vbclock
        move.w  _nvbls,d7
        beq.s   3f
        subq.w  #1,d7
        movea.l _vblqueue,a6
1:      move.l  (a6)+,d0
        beq.s   2f
        movea.l d0,a0
        movem.l d7/a6,-(sp)
        jsr     (a0)
        movem.l (sp)+,d7/a6
2:      dbra    d7,1b
3:      move.b  V_VBLPLAY,d0
        beq.s   4f
        bsr     call_play
4:      movem.l (sp)+,d0-d7/a0-a6
9:      addq.w  #1,_vblsem
        rte

; Level 2 autovector: HBL. Like TOS, raise the caller's mask to 3.
hbl:
        ori.w   #$0300,(sp)
        rte

; GEMDOS, BIOS and XBIOS. The host reads the arguments, performs the call and
; leaves the result for d0. Supexec has to run guest code, so it stays here.
trap_gemdos:
        movem.l d1/a0,-(sp)
        moveq   #1,d1
        bra.s   trap_common
trap_bios:
        movem.l d1/a0,-(sp)
        moveq   #13,d1
        bra.s   trap_common
trap_xbios:
        movem.l d1/a0,-(sp)
        moveq   #14,d1
trap_common:
        lea     14(sp),a0           ; arguments follow d1/a0, SR and PC
        btst    #5,8(sp)            ; called from supervisor mode?
        bne.s   1f
        move.l  usp,a0
1:      cmpi.w  #14,d1
        bne.s   2f
        cmpi.w  #38,(a0)            ; Supexec(func)
        beq.s   supexec
2:      move.l  a0,H_ARGS
        move.l  sp,H_FRAME
        move.w  d1,H_CALL
        move.l  H_RESULT,d0
        movem.l (sp)+,d1/a0
        rte
supexec:
        movea.l 2(a0),a0
        movem.l d1-d7/a1-a6,-(sp)
        jsr     (a0)
        movem.l (sp)+,d1-d7/a1-a6
        movem.l (sp)+,d1/a0
        rte

; Unexpected exception: report it and halt.
crash:
        move.w  #EV_CRASH,H_EVENT
1:      stop    #$2700
        bra.s   1b

nop_rte:
        rte
nop_rts:
        rts
