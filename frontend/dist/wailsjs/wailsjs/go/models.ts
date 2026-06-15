export namespace guiapp {
	
	export interface ProfileView {
	    currentUsername: string;
	    currentHome: string;
	    currentHostname: string;
	}
	export interface SetupRequest {
	
	}
	export interface SetupResult {
	    appliedUsername: string;
	    appliedHome: string;
	    appliedHostname: string;
	    rebootRequired: boolean;
	}

}

