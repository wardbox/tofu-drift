# shellcheck shell=bash
# Sourced by the sandbox scripts. Refuses to touch AWS until a human has
# seen the account ID and typed "yes", and refuses outright if that account
# is not the one the sandbox state was applied to.

: "${AWS_PROFILE:?set AWS_PROFILE explicitly to the throwaway account profile}"
export AWS_PROFILE TF_VAR_profile="$AWS_PROFILE"

cd "$(dirname "${BASH_SOURCE[0]}")"

TAG_KEY=tofu-drift-sandbox
PLANTED_KEY=tofu-drift-sandbox-planted

out() { tofu output -raw "$1"; }

confirm_account() {
	local account state_account answer
	account=$(aws sts get-caller-identity --query Account --output text)
	echo "AWS_PROFILE=$AWS_PROFILE"
	echo "Account:     $account"
	state_account=$(out account_id 2>/dev/null || true)
	if [[ -n $state_account && $state_account != "$account" ]]; then
		echo "Refusing: the sandbox state belongs to account $state_account, not $account." >&2
		exit 1
	fi
	read -r -p "$1 in account $account? Type yes to continue: " answer
	[[ $answer == yes ]] || {
		echo "Aborted." >&2
		exit 1
	}
}
