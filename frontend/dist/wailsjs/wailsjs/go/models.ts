export namespace guiapp {
	
	export interface ProfileView {
	    currentUsername: string;
	    currentHome: string;
	    currentHostname: string;
	}
	export interface SetupRequest {
	    changeAccount: boolean;
	    changeHostname: boolean;
	    username: string;
	    password: string;
	    hostname: string;
	}
	export interface SetupResult {
	    appliedUsername: string;
	    appliedHome: string;
	    appliedHostname: string;
	    rebootRequired: boolean;
	}

}
