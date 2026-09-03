package ym

// YM5/YM6 timer effects are encoded in otherwise-unused register bits. The
// scheme (from Arnaud Carré's ST-Sound):
//
//	YM5: r1 bits 4-5 select a voice for a timer-synth ("SID") square gate;
//	     r3 bits 4-5 select a voice for a digidrum. Timer rate comes from an
//	     MFP predivider index (r6/r8 bits 5-7) times a count (r14/r15).
//
//	YM6: two effect slots. Slot 1 packs {type,voice} into r1 bits 4-7 with the
//	     predivider in r6 bits 5-7 and count in r14; slot 2 uses r3/r8/r15.
//	     Types: 00=SID, 01=digidrum, 10=sinus-SID (played as SID), 11=sync-buzzer.
//
// Effect frequency = 2_457_600 / (mfpPrediv[idx] * count).

const mfpClock = 2_457_600

var mfpPrediv = [8]int{0, 4, 10, 16, 50, 64, 100, 200}

type effectReq struct {
	sid    [3]effSlot
	drum   [3]effSlot
	buzzer effSlot
}

type effSlot struct {
	on    bool
	freq  int
	shape byte // sync-buzzer envelope shape
}

func mfpFreq(predivIdx int, count byte) int {
	if predivIdx <= 0 || predivIdx >= len(mfpPrediv) || count == 0 {
		return 0
	}
	div := mfpPrediv[predivIdx] * int(count)
	if div == 0 {
		return 0
	}
	return mfpClock / div
}

func decodeEffects(version int, fb [16]byte) effectReq {
	var e effectReq
	switch version {
	case 6:
		decodeYM6Slot(&e, fb, 1, 6, 14)
		decodeYM6Slot(&e, fb, 3, 8, 15)
	case 5:
		if code := (fb[1] >> 4) & 3; code != 0 {
			v := int(code) - 1
			e.sid[v] = effSlot{on: true, freq: mfpFreq(int((fb[6]>>5)&7), fb[14])}
		}
		if code := (fb[3] >> 4) & 3; code != 0 {
			v := int(code) - 1
			e.drum[v] = effSlot{on: true, freq: mfpFreq(int((fb[8]>>5)&7), fb[15])}
		}
	}
	return e
}

func decodeYM6Slot(e *effectReq, fb [16]byte, codeReg, predivReg, countReg int) {
	code := fb[codeReg] & 0xf0
	if code&0x30 == 0 {
		return
	}
	v := int((code&0x30)>>4) - 1
	freq := mfpFreq(int((fb[predivReg]>>5)&7), fb[countReg])
	switch code & 0xc0 {
	case 0x00, 0x80: // SID / sinus-SID
		e.sid[v] = effSlot{on: true, freq: freq}
	case 0x40: // digidrum
		e.drum[v] = effSlot{on: true, freq: freq}
	case 0xc0: // sync-buzzer
		e.buzzer = effSlot{on: true, freq: freq, shape: fb[8+v] & 0x0f}
	}
}
