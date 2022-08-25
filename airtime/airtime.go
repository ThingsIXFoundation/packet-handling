package airtime

import (
	"fmt"
	"time"

	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan/airtime"
)

func UplinkAirtime(frame gw.UplinkFrame) (time.Duration, error) {
	payload := len(frame.PhyPayload)
	lora := frame.GetTxInfo().GetLoraModulationInfo()
	if lora == nil {
		return 0, fmt.Errorf("packet is not LoRa, cannot calculate airtime")
	}
	sf := int(lora.GetSpreadingFactor())
	bandwidth := int(lora.GetBandwidth())
	preamble := 8 // always 8 for LoRaWAN
	var codingrate airtime.CodingRate
	switch lora.CodeRate {
	case "4/5":
		codingrate = airtime.CodingRate45
	case "4/6":
		codingrate = airtime.CodingRate46
	case "4/7":
		codingrate = airtime.CodingRate47
	default:
		return 0, fmt.Errorf("invalid code rate: %s", lora.CodeRate)
	}
	headerEnabled := true
	lowDataRateOptimization := (sf >= 11)

	return airtime.CalculateLoRaAirtime(payload, sf, bandwidth, preamble, codingrate, headerEnabled, lowDataRateOptimization)
}

func DownlinkAirtime(frame gw.DownlinkFrame) (time.Duration, error) {
	payload := len(frame.PhyPayload)
	lora := frame.GetTxInfo().GetLoraModulationInfo()
	if lora == nil {
		return 0, fmt.Errorf("packet is not LoRa, cannot calculate airtime")
	}
	sf := int(lora.GetSpreadingFactor())
	bandwidth := int(lora.GetBandwidth())
	preamble := 8 // always 8 for LoRaWAN
	var codingrate airtime.CodingRate
	switch lora.CodeRate {
	case "4/5":
		codingrate = airtime.CodingRate45
	case "4/6":
		codingrate = airtime.CodingRate46
	case "4/7":
		codingrate = airtime.CodingRate47
	default:
		return 0, fmt.Errorf("invalid code rate: %s", lora.CodeRate)
	}
	headerEnabled := true
	lowDataRateOptimization := (sf >= 11)

	airtime, err := airtime.CalculateLoRaAirtime(payload, sf, bandwidth, preamble, codingrate, headerEnabled, lowDataRateOptimization)
	if err != nil {
		return 0, err
	}

	return airtime * 8, nil // For downlinks airtime is x 8 because all 8 channels of a gateway are claimed during transmission
}
