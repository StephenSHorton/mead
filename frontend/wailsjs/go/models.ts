export namespace main {
	
	export class BottleSummary {
	    id: string;
	    name: string;
	    created_at?: string;
	    wine_version?: string;
	
	    static createFrom(source: any = {}) {
	        return new BottleSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.created_at = source["created_at"];
	        this.wine_version = source["wine_version"];
	    }
	}

}

