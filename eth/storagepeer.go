// Copyright 2019 DxChain, All rights reserved.
// Use of this source code is governed by an Apache
// License 2.0 that can be found in the LICENSE file

package eth

import (
	"errors"
	"time"

	"github.com/DxChainNetwork/godx/p2p"
	"github.com/DxChainNetwork/godx/p2p/enode"
	"github.com/DxChainNetwork/godx/storage"
	"github.com/DxChainNetwork/godx/storage/coinchargemaintenance"
)

// TriggerError is used to send the error message to the errMsg channel,
// where the node will exit the readLoop and disconnect with the peer
func (p *peer) TriggerError(err error) {
	select {
	case p.errMsg <- err:
	default:
	}
}

// SendStorageHostConfig will send the storage host configuration to the client
// once the host got the request from the storage client
func (p *peer) SendStorageHostConfig(config storage.HostExtConfig) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostConfigRespMsg, config)
	}
	return err
}

// RequestStorageHostConfig is used when the client is trying to request host's
// configuration. The HostConfigReqMsg will be sent to the storage host
func (p *peer) RequestStorageHostConfig() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostConfigReqMsg, struct{}{})
	}
	return err
}

// RequestContractCreate will be used when the storage client is trying to create
// the contract with desired storage host. ContractCreateReqMsg will be sent to the
// storage host
func (p *peer) RequestContractCreation(req storage.ContractCreateRequest) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractCreateReqMsg, req)
	}
	return err
}

// SendContractCreateClientRevisionSig will be used once the storage client drafted and
// signed a contract revision and requesting the validation and signature from the storage host
func (p *peer) SendContractCreateClientRevisionSign(revisionSign []byte) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractCreateClientRevisionSign, revisionSign)
	}
	return err
}

// SendContractCreationHostSign will be used once the host received the ContractCreateReqMsg
// message from the client. The host will validated the contract, sign it, and sent back to
// the storage client
func (p *peer) SendContractCreationHostSign(contractSign []byte) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractCreateHostSign, contractSign)
	}
	return err
}

// SendContractCreationHostRevisionSign will be used once the host received the revised
// contract from the storage client. Host will validate it, sign it, and send it back
func (p *peer) SendContractCreationHostRevisionSign(revisionSign []byte) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractCreateRevisionSign, revisionSign)
	}
	return err
}

// RequestContractUpload is used when the client is trying to upload data
// to the corresponded storage host. Upload request must be sent to the storage
// host first
func (p *peer) RequestContractUpload(req storage.UploadRequest) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractUploadReqMsg, req)
	}
	return err
}

// SendContractUploadClientRevisionSign will be sent by the storage client
// once the client received the merkle proof sent by the storage host
func (p *peer) SendContractUploadClientRevisionSign(revisionSign []byte) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractUploadClientRevisionSign, revisionSign)
	}
	return err
}

// SendUploadMerkleProof is sent by the storage host to prove that it has the data
// that storage client needed
func (p *peer) SendUploadMerkleProof(merkleProof storage.UploadMerkleProof) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractUploadMerkleProofMsg, merkleProof)
	}
	return err
}

// SendUploadHostRevisionSign will be used once the storage host received the contract upload client
// revision sign sent by the storage client. Host will validate the revised contract, sign it, and
// send it back to the storage client
func (p *peer) SendUploadHostRevisionSign(revisionSign []byte) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractUploadRevisionSign, revisionSign)
	}
	return err
}

// RequestContractDownload will be used when the storage client wants to download
// data pieces from the corresponded storage host
func (p *peer) RequestContractDownload(req storage.DownloadRequest) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractDownloadReqMsg, req)
	}
	return err
}

// SendContractDownloadData is sent by the client. Data piece requested by the
// storage client will be included
func (p *peer) SendContractDownloadData(resp storage.DownloadResponse) error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ContractDownloadDataMsg, resp)
	}
	return err
}

// SendHostBusyHandleRequestErr will send a error message to client, stating that
// the host is currently busy handling the previous error message
func (p *peer) SendHostBusyHandleRequestErr() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostBusyHandleReqMsg, "error handling")
	}
	return err
}

// SendClientNegotiateErrorMsg will send client negotiate error msg
func (p *peer) SendClientNegotiateErrorMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ClientNegotiateErrorMsg, storage.ErrClientNegotiate.Error())
	}
	return err
}

// SendClientCommitFailedMsg will send a error msg to Host, indicating that client occurs exception
// when executing 'Commit Action'
func (p *peer) SendClientCommitFailedMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ClientCommitFailedMsg, storage.ErrClientCommit.Error())
	}
	return err
}

// SendClientCommitSuccessMsg will send a success msg to Host, indicating that client has no error after 'Commit Action'
func (p *peer) SendClientCommitSuccessMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ClientCommitSuccessMsg, "commit success")
	}
	return err
}

// SendClientCommitSuccessMsg will send host commit failed msg to client
func (p *peer) SendHostCommitFailedMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostCommitFailedMsg, storage.ErrHostCommit.Error())
	}
	return err
}

func (p *peer) SendClientAckMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.ClientAckMsg, "client ack")
	}
	return err
}

// SendHostAckMsg will send host ack msg to client as the last negotiate msg no matter what success or failed
func (p *peer) SendHostAckMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostAckMsg, "host ack")
	}
	return err
}

// SendHostNegotiateErrorMsg will send host negotiate error msg
func (p *peer) SendHostNegotiateErrorMsg() error {
	var err error
	if err = p.checkPeerStopHook(p); err == nil {
		return p2p.Send(p.rw, storage.HostNegotiateErrorMsg, storage.ErrHostNegotiate.Error())
	}
	return err
}

// WaitConfigResp is used by the storage client, waiting from the configuration
// response from the storage host
func (p *peer) WaitConfigResp() (msg p2p.Msg, err error) {
	timeout := time.After(1 * time.Minute)
	select {
	case msg = <-p.clientConfigMsg:
		return
	case <-timeout:
		err = errors.New("timeout -> client waits too long for config response from the host")
		return
	case <-p.StopChan():
		err = coinchargemaintenance.ErrProgramExit
		return
	}
}

// ClientWaitContractResp is used by the storage client. The method will block the current
// process until the response was sent back from the storage host
func (p *peer) ClientWaitContractResp() (msg p2p.Msg, err error) {
	timeout := time.After(1 * time.Minute)
	select {
	case msg = <-p.clientContractMsg:
		return
	case <-timeout:
		err = errors.New("timeout -> client waits too long for contract response from the host")
		return
	case <-p.StopChan():
		err = coinchargemaintenance.ErrProgramExit
		return
	}
}

// HostWaitContractResp is used by the storage host. The method will block the current
// process until the response was sent back from the storage client
func (p *peer) HostWaitContractResp() (msg p2p.Msg, err error) {
	timeout := time.After(1 * time.Minute)
	select {
	case msg = <-p.hostContractMsg:
		return
	case <-timeout:
		err = errors.New("timeout -> host waits too long for contract response from the host")
		return
	case <-p.StopChan():
		err = coinchargemaintenance.ErrProgramExit
		return
	}
}

// HostConfigProcessing is used to indicate that the host is currently processing
// the storage host configuration request sent from the storage client, which will
// deny another configuration request sent by the storage client
func (p *peer) HostConfigProcessing() error {
	select {
	case p.hostConfigProcessing <- struct{}{}:
		return nil
	default:
		return errors.New("host config request is currently processing, please wait until it finished first")
	}
}

// HostConfigProcessingDone is used to indicate that storage host finished processing
// the storage host configuration
func (p *peer) HostConfigProcessingDone() {
	select {
	case <-p.hostConfigProcessing:
		return
	default:
		p.Log().Warn("host config processing finished before it is actually done")
	}
}

// HostContractProcessing is used to indicate that the host is currently processing
// the contract related request sent from the storage client. It will include data upload,
// data download, contract creation, and contract revision
func (p *peer) HostContractProcessing() error {
	select {
	case p.hostContractProcessing <- struct{}{}:
		return nil
	default:
		return errors.New("host contract related operation is currently processing, please wait until it finished first")
	}
}

// HostContractProcessingDone is used to indicate that storage host finished processing
// the client's contract request, and is ready for the next request
func (p *peer) HostContractProcessingDone() {
	select {
	case <-p.hostContractProcessing:
		return
	default:
		p.Log().Warn("host contract processing finished before it is actually done")
	}
}

// TryToRenewOrRevise will try to renew or revise the contract, if failed
// the renew process and revision process will be interrupted immediately
func (p *peer) TryToRenewOrRevise() bool {
	select {
	case p.contractRevisingOrRenewing <- struct{}{}:
		return true
	default:
		return false
	}
}

// RevisionOrRenewingDone indicates the revision or renewing operation has been finished
func (p *peer) RevisionOrRenewingDone() {
	select {
	case <-p.contractRevisingOrRenewing:
	default:
	}
}

// TryRequestHostConfig is used to check if the client is currently requesting storage
// client configuration, meaning the client should not send another request message
// before the previous request has finished
func (p *peer) TryRequestHostConfig() error {
	select {
	case p.hostConfigRequesting <- struct{}{}:
		return nil
	default:
		return storage.ErrRequestingHostConfig
	}
}

// RequestHostConfigDone is used to indicate the storage client
// that the storage config request is finished
func (p *peer) RequestHostConfigDone() {
	select {
	case <-p.hostConfigRequesting:
	default:
	}
}

// IsStaticConn checks if the connection is static connection
func (p *peer) IsStaticConn() bool {
	return p.Peer.Info().Network.Static
}

// PeerNode returns the peer's node information
func (p *peer) PeerNode() *enode.Node {
	return p.Peer.Node()
}
