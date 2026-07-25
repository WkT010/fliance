package wallet

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type AlchemyClient struct {
	asset      string
	rpcURL     string
	httpClient *http.Client
}

func NewAlchemyClient(asset, rpcURL string) *AlchemyClient {
	return &AlchemyClient{asset: strings.ToUpper(asset), rpcURL: rpcURL, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

type rpcReq struct{ JSONRPC, Method string; Params []interface{}; ID int }
type rpcResp struct{ JSONRPC string; ID int; Result json.RawMessage; Error *struct{ Code int; Message string } }

func (a *AlchemyClient) call(method string, params []interface{}) (json.RawMessage, error) {
	b, _ := json.Marshal(rpcReq{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	req, _ := http.NewRequest("POST", a.rpcURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil { return nil, fmt.Errorf("rpc %s: %w", method, err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r rpcResp
	json.Unmarshal(body, &r)
	if r.Error != nil { return nil, fmt.Errorf("alchemy err %d: %s", r.Error.Code, r.Error.Message) }
	return r.Result, nil
}

func (a *AlchemyClient) GenerateAddress() (string, error) {
	return "", fmt.Errorf("alchemy: GenerateAddress requires HD wallet (e.g., go-ethereum/crypto)")
}

func (a *AlchemyClient) GetBalance(address string) (*big.Float, error) {
	result, err := a.call("eth_getBalance", []interface{}{address, "latest"})
	if err != nil { return nil, err }
	var hexBal string
	json.Unmarshal(result, &hexBal)
	wei := new(big.Int)
	wei.SetString(strings.TrimPrefix(hexBal, "0x"), 16)
	eth := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	return eth, nil
}

func (a *AlchemyClient) SendTransaction(to string, amount *big.Float) (string, error) {
	wei := new(big.Int)
	new(big.Float).Mul(amount, big.NewFloat(1e18)).Int(wei)
	_, err := a.call("eth_estimateGas", []interface{}{map[string]interface{}{"to": to, "value": "0x" + hex.EncodeToString(wei.Bytes())}})
	if err != nil { return "", fmt.Errorf("estimate: %w", err) }
	return "", fmt.Errorf("alchemy: requires local tx signing + eth_sendRawTransaction")
}

func (a *AlchemyClient) GetConfirmations(txHash string) (int, error) {
	result, err := a.call("eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil { return 0, err }
	var r struct{ BlockNumber string }
	json.Unmarshal(result, &r)
	if r.BlockNumber == "" { return 0, nil }
	blockR, _ := a.call("eth_blockNumber", nil)
	var cur string; json.Unmarshal(blockR, &cur)
	tb := new(big.Int); tb.SetString(strings.TrimPrefix(r.BlockNumber, "0x"), 16)
	cb := new(big.Int); cb.SetString(strings.TrimPrefix(cur, "0x"), 16)
	return int(new(big.Int).Sub(cb, tb).Int64()), nil
}

func (a *AlchemyClient) IsValidAddress(addr string) bool {
	if !strings.HasPrefix(addr, "0x") { return false }
	a = strings.TrimPrefix(addr, "0x")
	if len(a) != 40 { return false }
	_, err := hex.DecodeString(a)
	return err == nil
}

var _ BlockchainClient = (*AlchemyClient)(nil)
