package application

import (
	"encoding/json"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	"io"
	"os"

	"github.com/persistenceOne/persistenceSDK/types"

	"github.com/persistenceOne/persistenceSDK/modules/contract"
	"github.com/persistenceOne/persistenceSDK/modules/escrow"
	"github.com/persistenceOne/persistenceSDK/modules/reputation"
	"github.com/persistenceOne/persistenceSDK/modules/share"

	"github.com/persistenceOne/persistenceSDK/modules/asset"

	abciTypes "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tendermintOS "github.com/tendermint/tendermint/libs/os"
	tendermintTypes "github.com/tendermint/tendermint/types"
	tendermintDB "github.com/tendermint/tm-db"
	"honnef.co/go/tools/version"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdkTypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/gov"
	"github.com/cosmos/cosmos-sdk/x/mint"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsClient "github.com/cosmos/cosmos-sdk/x/params/client"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/supply"
)

const applicationName = "PersistenceCore"

var DefaultClientHome = os.ExpandEnv("$HOME/.coreClient")
var DefaultNodeHome = os.ExpandEnv("$HOME/.coreNode")
var moduleAccountPermissions = map[string][]string{
	auth.FeeCollectorName:     nil,
	distribution.ModuleName:   nil,
	mint.ModuleName:           {supply.Minter},
	staking.BondedPoolName:    {supply.Burner, supply.Staking},
	staking.NotBondedPoolName: {supply.Burner, supply.Staking},
	gov.ModuleName:            {supply.Burner},
}
var tokenReceiveAllowedModules = map[string]bool{
	distribution.ModuleName: true,
}
var ModuleBasics = module.NewBasicManager(
	genutil.AppModuleBasic{},
	auth.AppModuleBasic{},
	bank.AppModuleBasic{},
	staking.AppModuleBasic{},
	mint.AppModuleBasic{},
	distribution.AppModuleBasic{},
	gov.NewAppModuleBasic(paramsClient.ProposalHandler, distribution.ProposalHandler),
	params.AppModuleBasic{},
	crisis.AppModuleBasic{},
	slashing.AppModuleBasic{},
	supply.AppModuleBasic{},
	asset.AppModuleBasic{},
	reputation.AppModuleBasic{},
	contract.AppModuleBasic{},
	escrow.AppModuleBasic{},
	share.AppModuleBasic{},
)

type GenesisState map[string]json.RawMessage

func NewDefaultGenesisState() GenesisState {
	return ModuleBasics.DefaultGenesis()
}

func MakeCodec() *codec.Codec {
	var Codec = codec.New()
	ModuleBasics.RegisterCodec(Codec)
	sdkTypes.RegisterCodec(Codec)
	types.RegisterCodec(Codec)
	codec.RegisterCrypto(Codec)
	codec.RegisterEvidences(Codec)
	return Codec
}

type PersistenceHubApplication struct {
	*baseapp.BaseApp
	codec *codec.Codec

	invCheckPeriod uint

	keys          map[string]*sdkTypes.KVStoreKey
	transientKeys map[string]*sdkTypes.TransientStoreKey

	subspaces map[string]params.Subspace

	accountKeeper      auth.AccountKeeper
	bankKeeper         bank.Keeper
	supplyKeeper       supply.Keeper
	stakingKeeper      staking.Keeper
	slashingKeeper     slashing.Keeper
	mintKeeper         mint.Keeper
	distributionKeeper distribution.Keeper
	govKeeper          gov.Keeper
	crisisKeeper       crisis.Keeper
	upgradeKeeper      upgrade.Keeper
	parameterKeeper    params.Keeper
	evidenceKeeper     evidence.Keeper

	assetKeeper      asset.Keeper
	reputationKeeper reputation.Keeper
	contractKeeper   contract.Keeper
	escrowKeeper     escrow.Keeper
	shareKeeper      share.Keeper

	moduleManager *module.Manager

	simulationManager *module.SimulationManager
}

func NewPersistenceHubApplication(
	logger log.Logger,
	db tendermintDB.DB,
	traceStore io.Writer,
	loadLatest bool,
	invCheckPeriod uint,
	skipUpgradeHeights map[int64]bool,
	home string,
	baseAppOptions ...func(*baseapp.BaseApp),
) *PersistenceHubApplication {

	codec := MakeCodec()

	baseApp := baseapp.NewBaseApp(
		applicationName,
		logger,
		db,
		auth.DefaultTxDecoder(codec),
		baseAppOptions...,
	)
	baseApp.SetCommitMultiStoreTracer(traceStore)
	baseApp.SetAppVersion(version.Version)

	keys := sdkTypes.NewKVStoreKeys(
		baseapp.MainStoreKey,
		auth.StoreKey,
		bank.ModuleName,
		staking.StoreKey,
		supply.StoreKey,
		mint.StoreKey,
		distribution.StoreKey,
		slashing.StoreKey,
		gov.StoreKey,
		params.StoreKey,
		evidence.StoreKey,
		upgrade.StoreKey,

		asset.StoreKey,
		reputation.StoreKey,
		contract.StoreKey,
		escrow.StoreKey,
		share.StoreKey,
	)
	transientKeys := sdkTypes.NewTransientStoreKeys(
		staking.TStoreKey,
		params.TStoreKey,
	)

	var application = &PersistenceHubApplication{
		BaseApp:        baseApp,
		codec:          codec,
		invCheckPeriod: invCheckPeriod,
		keys:           keys,
		transientKeys:  transientKeys,
		subspaces:      make(map[string]params.Subspace),
	}

	application.parameterKeeper = params.NewKeeper(
		application.codec,
		keys[params.StoreKey],
		transientKeys[params.TStoreKey],
	)
	application.subspaces[auth.ModuleName] = application.parameterKeeper.Subspace(auth.DefaultParamspace)
	application.subspaces[bank.ModuleName] = application.parameterKeeper.Subspace(bank.DefaultParamspace)
	application.subspaces[staking.ModuleName] = application.parameterKeeper.Subspace(staking.DefaultParamspace)
	application.subspaces[mint.ModuleName] = application.parameterKeeper.Subspace(mint.DefaultParamspace)
	application.subspaces[distribution.ModuleName] = application.parameterKeeper.Subspace(distribution.DefaultParamspace)
	application.subspaces[slashing.ModuleName] = application.parameterKeeper.Subspace(slashing.DefaultParamspace)
	application.subspaces[gov.ModuleName] = application.parameterKeeper.Subspace(gov.DefaultParamspace)
	application.subspaces[crisis.ModuleName] = application.parameterKeeper.Subspace(crisis.DefaultParamspace)
	application.subspaces[evidence.ModuleName] = application.parameterKeeper.Subspace(evidence.DefaultParamspace)

	application.subspaces[asset.ModuleName] = application.parameterKeeper.Subspace(asset.DefaultParamspace)
	application.subspaces[reputation.ModuleName] = application.parameterKeeper.Subspace(reputation.DefaultParamspace)
	application.subspaces[contract.ModuleName] = application.parameterKeeper.Subspace(contract.DefaultParamspace)
	application.subspaces[escrow.ModuleName] = application.parameterKeeper.Subspace(escrow.DefaultParamspace)
	application.subspaces[share.ModuleName] = application.parameterKeeper.Subspace(share.DefaultParamspace)

	application.accountKeeper = auth.NewAccountKeeper(
		application.codec,
		keys[auth.StoreKey],
		application.subspaces[auth.ModuleName],
		auth.ProtoBaseAccount,
	)

	application.bankKeeper = bank.NewBaseKeeper(
		application.accountKeeper,
		application.subspaces[bank.ModuleName],
		application.BlacklistedAccAddrs(),
	)

	application.supplyKeeper = supply.NewKeeper(
		application.codec,
		keys[supply.StoreKey],
		application.accountKeeper,
		application.bankKeeper,
		moduleAccountPermissions,
	)

	stakingKeeper := staking.NewKeeper(
		application.codec,
		keys[staking.StoreKey],
		application.supplyKeeper,
		application.subspaces[staking.ModuleName],
	)
	application.mintKeeper = mint.NewKeeper(
		application.codec,
		keys[mint.StoreKey],
		application.subspaces[mint.ModuleName],
		&stakingKeeper,
		application.supplyKeeper,
		auth.FeeCollectorName,
	)
	application.distributionKeeper = distribution.NewKeeper(
		application.codec,
		keys[distribution.StoreKey],
		application.subspaces[distribution.ModuleName],
		&stakingKeeper,
		application.supplyKeeper,
		auth.FeeCollectorName,
		application.ModuleAccountAddress(),
	)
	application.slashingKeeper = slashing.NewKeeper(
		application.codec,
		keys[slashing.StoreKey],
		&stakingKeeper,
		application.subspaces[slashing.ModuleName],
	)
	application.crisisKeeper = crisis.NewKeeper(
		application.subspaces[crisis.ModuleName],
		invCheckPeriod,
		application.supplyKeeper,
		auth.FeeCollectorName,
	)
	application.upgradeKeeper = upgrade.NewKeeper(
		skipUpgradeHeights,
		keys[upgrade.StoreKey],
		codec,
	)
	evidenceKeeper := evidence.NewKeeper(
		codec,
		keys[evidence.StoreKey],
		application.subspaces[evidence.ModuleName],
		&stakingKeeper,
		application.slashingKeeper,
	)
	evidenceRouter := evidence.NewRouter()
	evidenceKeeper.SetRouter(evidenceRouter)
	application.evidenceKeeper = *evidenceKeeper
	govRouter := gov.NewRouter()
	govRouter.AddRoute(
		gov.RouterKey,
		gov.ProposalHandler,
	).AddRoute(
		params.RouterKey,
		params.NewParamChangeProposalHandler(application.parameterKeeper),
	).AddRoute(
		distribution.RouterKey,
		distribution.NewCommunityPoolSpendProposalHandler(application.distributionKeeper),
	).AddRoute(
		upgrade.RouterKey,
		upgrade.NewSoftwareUpgradeProposalHandler(application.upgradeKeeper),
	)
	application.govKeeper = gov.NewKeeper(
		application.codec,
		keys[gov.StoreKey],
		application.subspaces[gov.ModuleName],
		application.supplyKeeper,
		&stakingKeeper,
		govRouter,
	)

	application.stakingKeeper = *stakingKeeper.SetHooks(
		staking.NewMultiStakingHooks(
			application.distributionKeeper.Hooks(),
			application.slashingKeeper.Hooks(),
		),
	)

	application.assetKeeper = asset.NewKeeper(
		application.codec,
		keys[asset.StoreKey],
		application.subspaces[asset.ModuleName],
	)
	application.reputationKeeper = reputation.NewKeeper(
		application.codec,
		keys[asset.StoreKey],
		application.subspaces[reputation.ModuleName],
	)
	application.contractKeeper = contract.NewKeeper(
		application.codec,
		keys[asset.StoreKey],
		application.subspaces[contract.ModuleName],
	)
	application.escrowKeeper = escrow.NewKeeper(
		application.codec,
		keys[asset.StoreKey],
		application.subspaces[escrow.ModuleName],
	)
	application.shareKeeper = share.NewKeeper(
		application.codec, keys[share.StoreKey],
		application.subspaces[share.ModuleName],
	)

	application.moduleManager = module.NewManager(
		genutil.NewAppModule(application.accountKeeper, application.stakingKeeper, application.BaseApp.DeliverTx),
		auth.NewAppModule(application.accountKeeper),
		bank.NewAppModule(application.bankKeeper, application.accountKeeper),
		crisis.NewAppModule(&application.crisisKeeper),
		supply.NewAppModule(application.supplyKeeper, application.accountKeeper),
		gov.NewAppModule(application.govKeeper, application.accountKeeper, application.supplyKeeper),
		mint.NewAppModule(application.mintKeeper),
		slashing.NewAppModule(application.slashingKeeper, application.accountKeeper, application.stakingKeeper),
		distribution.NewAppModule(application.distributionKeeper, application.accountKeeper, application.supplyKeeper, application.stakingKeeper),
		staking.NewAppModule(application.stakingKeeper, application.accountKeeper, application.supplyKeeper),
		upgrade.NewAppModule(application.upgradeKeeper),
		evidence.NewAppModule(application.evidenceKeeper),

		asset.NewAppModule(application.assetKeeper),
		reputation.NewAppModule(application.reputationKeeper),
		contract.NewAppModule(application.contractKeeper),
		escrow.NewAppModule(application.escrowKeeper),
		share.NewAppModule(application.shareKeeper),
	)
	application.moduleManager.SetOrderBeginBlockers(
		upgrade.ModuleName,
		mint.ModuleName,
		distribution.ModuleName,
		slashing.ModuleName,
	)
	application.moduleManager.SetOrderEndBlockers(
		crisis.ModuleName,
		gov.ModuleName,
		staking.ModuleName,
	)
	application.moduleManager.SetOrderInitGenesis(
		distribution.ModuleName,
		staking.ModuleName,
		auth.ModuleName,
		bank.ModuleName,
		slashing.ModuleName,
		gov.ModuleName,
		mint.ModuleName,
		supply.ModuleName,
		crisis.ModuleName,
		genutil.ModuleName,
		evidence.ModuleName,

		asset.ModuleName,
		reputation.ModuleName,
		contract.ModuleName,
		escrow.ModuleName,
		share.ModuleName,
	)
	application.moduleManager.RegisterInvariants(&application.crisisKeeper)
	application.moduleManager.RegisterRoutes(application.Router(), application.QueryRouter())

	//TODO add peristenceSDK modules to simulation
	application.simulationManager = module.NewSimulationManager(
		auth.NewAppModule(application.accountKeeper),
		bank.NewAppModule(application.bankKeeper, application.accountKeeper),
		supply.NewAppModule(application.supplyKeeper, application.accountKeeper),
		gov.NewAppModule(application.govKeeper, application.accountKeeper, application.supplyKeeper),
		mint.NewAppModule(application.mintKeeper),
		slashing.NewAppModule(application.slashingKeeper, application.accountKeeper, application.stakingKeeper),
		distribution.NewAppModule(application.distributionKeeper, application.accountKeeper, application.supplyKeeper, application.stakingKeeper),
		staking.NewAppModule(application.stakingKeeper, application.accountKeeper, application.supplyKeeper),
	)
	application.simulationManager.RegisterStoreDecoders()

	application.MountKVStores(keys)
	application.MountTransientStores(transientKeys)

	application.SetInitChainer(application.InitChainer)
	application.SetBeginBlocker(application.BeginBlocker)
	application.SetAnteHandler(auth.NewAnteHandler(application.accountKeeper, application.supplyKeeper, auth.DefaultSigVerificationGasConsumer))
	application.SetEndBlocker(application.EndBlocker)

	if loadLatest {
		err := application.LoadLatestVersion(application.keys[baseapp.MainStoreKey])
		if err != nil {
			tendermintOS.Exit(err.Error())
		}
	}

	return application
}
func (application *PersistenceHubApplication) BeginBlocker(ctx sdkTypes.Context, req abciTypes.RequestBeginBlock) abciTypes.ResponseBeginBlock {
	return application.moduleManager.BeginBlock(ctx, req)
}
func (application *PersistenceHubApplication) EndBlocker(ctx sdkTypes.Context, req abciTypes.RequestEndBlock) abciTypes.ResponseEndBlock {
	return application.moduleManager.EndBlock(ctx, req)
}
func (application *PersistenceHubApplication) InitChainer(ctx sdkTypes.Context, req abciTypes.RequestInitChain) abciTypes.ResponseInitChain {
	var genesisState GenesisState
	application.codec.MustUnmarshalJSON(req.AppStateBytes, &genesisState)
	return application.moduleManager.InitGenesis(ctx, genesisState)
}
func (application *PersistenceHubApplication) LoadHeight(height int64) error {
	return application.LoadVersion(height, application.keys[baseapp.MainStoreKey])
}
func (application *PersistenceHubApplication) ModuleAccountAddress() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range moduleAccountPermissions {
		modAccAddrs[supply.NewModuleAddress(acc).String()] = true
	}

	return modAccAddrs
}
func (application *PersistenceHubApplication) ExportApplicationStateAndValidators(forZeroHeight bool, jailWhiteList []string,
) (applicationState json.RawMessage, validators []tendermintTypes.GenesisValidator, err error) {
	ctx := application.NewContext(true, abciTypes.Header{Height: application.LastBlockHeight()})

	if forZeroHeight {
		application.prepareForZeroHeightGenesis(ctx, jailWhiteList)
	}

	genesisState := application.moduleManager.ExportGenesis(ctx)
	applicationState, err = codec.MarshalJSONIndent(application.codec, genesisState)
	if err != nil {
		return nil, nil, err
	}
	validators = staking.WriteValidators(ctx, application.stakingKeeper)
	return applicationState, validators, nil
}
func (application *PersistenceHubApplication) BlacklistedAccAddrs() map[string]bool {
	blacklistedAddresses := make(map[string]bool)
	for account := range moduleAccountPermissions {
		blacklistedAddresses[supply.NewModuleAddress(account).String()] = !tokenReceiveAllowedModules[account]
	}

	return blacklistedAddresses
}

func (application *PersistenceHubApplication) prepareForZeroHeightGenesis(ctx sdkTypes.Context, jailWhiteList []string) {
	applyWhiteList := false

	if len(jailWhiteList) > 0 {
		applyWhiteList = true
	}

	whiteListMap := make(map[string]bool)

	for _, address := range jailWhiteList {
		_, err := sdkTypes.ValAddressFromBech32(address)
		if err != nil {
			//log.Fatal(err) //todo
		}
		whiteListMap[address] = true
	}

	application.crisisKeeper.AssertInvariants(ctx)

	application.stakingKeeper.IterateValidators(ctx, func(_ int64, val staking.ValidatorI) (stop bool) {
		_, _ = application.distributionKeeper.WithdrawValidatorCommission(ctx, val.GetOperator())
		return false
	})

	delegations := application.stakingKeeper.GetAllDelegations(ctx)
	for _, delegation := range delegations {
		_, _ = application.distributionKeeper.WithdrawDelegationRewards(ctx, delegation.DelegatorAddress, delegation.ValidatorAddress)
	}

	application.distributionKeeper.DeleteAllValidatorSlashEvents(ctx)

	application.distributionKeeper.DeleteAllValidatorHistoricalRewards(ctx)

	height := ctx.BlockHeight()
	ctx = ctx.WithBlockHeight(0)

	application.stakingKeeper.IterateValidators(ctx, func(_ int64, val staking.ValidatorI) (stop bool) {

		scraps := application.distributionKeeper.GetValidatorOutstandingRewards(ctx, val.GetOperator())
		feePool := application.distributionKeeper.GetFeePool(ctx)
		feePool.CommunityPool = feePool.CommunityPool.Add(scraps...)
		application.distributionKeeper.SetFeePool(ctx, feePool)

		application.distributionKeeper.Hooks().AfterValidatorCreated(ctx, val.GetOperator())
		return false
	})

	for _, delegation := range delegations {
		application.distributionKeeper.Hooks().BeforeDelegationCreated(ctx, delegation.DelegatorAddress, delegation.ValidatorAddress)
		application.distributionKeeper.Hooks().AfterDelegationModified(ctx, delegation.DelegatorAddress, delegation.ValidatorAddress)
	}

	ctx = ctx.WithBlockHeight(height)

	application.stakingKeeper.IterateRedelegations(ctx, func(_ int64, redelegation staking.Redelegation) (stop bool) {
		for i := range redelegation.Entries {
			redelegation.Entries[i].CreationHeight = 0
		}
		application.stakingKeeper.SetRedelegation(ctx, redelegation)
		return false
	})

	application.stakingKeeper.IterateUnbondingDelegations(ctx, func(_ int64, unbondingDelegation staking.UnbondingDelegation) (stop bool) {
		for i := range unbondingDelegation.Entries {
			unbondingDelegation.Entries[i].CreationHeight = 0
		}
		application.stakingKeeper.SetUnbondingDelegation(ctx, unbondingDelegation)
		return false
	})

	store := ctx.KVStore(application.keys[staking.StoreKey])
	kvStoreReversePrefixIterator := sdkTypes.KVStoreReversePrefixIterator(store, staking.ValidatorsKey)
	counter := int16(0)

	var validatorConsAddress []sdkTypes.ConsAddress
	for ; kvStoreReversePrefixIterator.Valid(); kvStoreReversePrefixIterator.Next() {
		addr := sdkTypes.ValAddress(kvStoreReversePrefixIterator.Key()[1:])
		validator, found := application.stakingKeeper.GetValidator(ctx, addr)
		if !found {
			panic("Validator not found!")
		}

		validator.UnbondingHeight = 0
		validatorConsAddress = append(validatorConsAddress, validator.ConsAddress())
		if applyWhiteList && !whiteListMap[addr.String()] {
			validator.Jailed = true
		}

		application.stakingKeeper.SetValidator(ctx, validator)
		counter++
	}

	kvStoreReversePrefixIterator.Close()

	_ = application.stakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)

	application.slashingKeeper.IterateValidatorSigningInfos(
		ctx,
		func(validatorConsAddress sdkTypes.ConsAddress, validatorSigningInfo slashing.ValidatorSigningInfo) (stop bool) {
			validatorSigningInfo.StartHeight = 0
			application.slashingKeeper.SetValidatorSigningInfo(ctx, validatorConsAddress, validatorSigningInfo)
			return false
		},
	)
}
