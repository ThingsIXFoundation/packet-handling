package structs

import (
	"testing"

	"github.com/brocaar/lorawan"
	"github.com/brocaar/lorawan/band"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/stretchr/testify/require"
)

func TestUplinkDataFrameToProto(t *testing.T) {
	assert := require.New(t)

	tests := []struct {
		Name  string
		In    UplinkDataFrame
		Out   *gw.UplinkFrame
		Error error
	}{
		{
			Name: "No FPort and FRMPayload",
			In: UplinkDataFrame{
				RadioMetaData: RadioMetaData{
					DR:        5,
					Frequency: 868100000,
					UpInfo: RadioMetaDataUpInfo{
						RCtx:  1,
						XTime: 2,
						RSSI:  120,
						SNR:   5.5,
					},
				},
				MessageType: UplinkDataFrameMessage,
				MHDR:        0x40, // unconfirmed data-up
				DevAddr:     -10,
				FCtrl:       0x80, // ADR
				FCnt:        400,
				FOpts:       "0102", // invalid, but for the purpose of testing
				MIC:         -20,
				FPort:       -1,
			},
			Out: &gw.UplinkFrame{
				PhyPayload: []byte{0x40, 0xf6, 0xff, 0xff, 0x0ff, 0x80, 0x90, 0x01, 0x01, 0x02, 0xec, 0xff, 0xff, 0xff},
				TxInfo: &gw.UplinkTxInfo{
					Frequency: 868100000,
					Modulation: &gw.Modulation{
						Parameters: &gw.Modulation_Lora{
							Lora: &gw.LoraModulationInfo{
								Bandwidth:       125000,
								SpreadingFactor: 7,
								CodeRate:        gw.CodeRate_CR_4_5,
							},
						},
					},
				},
				RxInfo: &gw.UplinkRxInfo{
					GatewayId: "0102030405060708",
					Rssi:      120,
					Snr:       5.5,
					Context:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
				},
			},
		},
		{
			Name: "FPort no FRMPayload",
			In: UplinkDataFrame{
				RadioMetaData: RadioMetaData{
					DR:        5,
					Frequency: 868100000,
					UpInfo: RadioMetaDataUpInfo{
						RCtx:  1,
						XTime: 2,
						RSSI:  120,
						SNR:   5.5,
					},
				},
				MessageType: UplinkDataFrameMessage,
				MHDR:        0x40, // unconfirmed data-up
				DevAddr:     -10,
				FCtrl:       0x80, // ADR
				FCnt:        400,
				FOpts:       "0102", // invalid, but for the purpose of testing
				MIC:         -20,
				FPort:       1,
			},
			Out: &gw.UplinkFrame{
				PhyPayload: []byte{0x40, 0xf6, 0xff, 0xff, 0x0ff, 0x80, 0x90, 0x01, 0x01, 0x02, 0x01, 0xec, 0xff, 0xff, 0xff},
				TxInfo: &gw.UplinkTxInfo{
					Frequency: 868100000,
					Modulation: &gw.Modulation{
						Parameters: &gw.Modulation_Lora{
							Lora: &gw.LoraModulationInfo{
								Bandwidth:       125000,
								SpreadingFactor: 7,
								CodeRate:        gw.CodeRate_CR_4_5,
							},
						},
					},
				},
				RxInfo: &gw.UplinkRxInfo{
					GatewayId: "0102030405060708",
					Rssi:      120,
					Snr:       5.5,
					Context:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
				},
			},
		},
		{
			Name: "FPort and FRMPayload",
			In: UplinkDataFrame{
				RadioMetaData: RadioMetaData{
					DR:        5,
					Frequency: 868100000,
					UpInfo: RadioMetaDataUpInfo{
						RCtx:  1,
						XTime: 2,
						RSSI:  120,
						SNR:   5.5,
					},
				},
				MessageType: UplinkDataFrameMessage,
				MHDR:        0x40, // unconfirmed data-up
				DevAddr:     -10,
				FCtrl:       0x80, // ADR
				FCnt:        400,
				FOpts:       "0102", // invalid, but for the purpose of testing
				MIC:         -20,
				FPort:       1,
				FRMPayload:  "04030201",
			},
			Out: &gw.UplinkFrame{
				PhyPayload: []byte{0x40, 0xf6, 0xff, 0xff, 0x0ff, 0x80, 0x90, 0x01, 0x01, 0x02, 0x01, 0x04, 0x03, 0x02, 0x01, 0xec, 0xff, 0xff, 0xff},
				TxInfo: &gw.UplinkTxInfo{
					Frequency: 868100000,
					Modulation: &gw.Modulation{
						Parameters: &gw.Modulation_Lora{
							Lora: &gw.LoraModulationInfo{
								Bandwidth:       125000,
								SpreadingFactor: 7,
								CodeRate:        gw.CodeRate_CR_4_5,
							},
						},
					},
				},
				RxInfo: &gw.UplinkRxInfo{
					GatewayId: "0102030405060708",
					Rssi:      120,
					Snr:       5.5,
					Context:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
				},
			},
		},
	}

	b, err := band.GetConfig(band.EU868, false, lorawan.DwellTimeNoLimit)
	assert.NoError(err)

	for _, tst := range tests {
		assert := require.New(t)

		uf, err := UplinkDataFrameToProto(b, lorawan.EUI64{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, tst.In)
		assert.Equal(tst.Error, err)
		if err != nil {
			return
		}
		assert.Equal(tst.Out, uf)
	}
}
