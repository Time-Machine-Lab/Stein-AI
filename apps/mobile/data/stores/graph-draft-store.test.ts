import { beforeEach, describe, expect, it, vi } from "vitest";
vi.mock("expo-secure-store", () => ({ getItemAsync: vi.fn().mockResolvedValue(null), setItemAsync: vi.fn().mockResolvedValue(undefined), deleteItemAsync: vi.fn().mockResolvedValue(undefined) }));
import { useGraphDraftStore, graphDraftIdentity } from "./graph-draft-store";
const def = { schemaVersion: 2, name: "Research", inputSchema: { type: "object" }, nodes: [{ nodeKey: "start", type: "start" }], edges: [] };
beforeEach(() => useGraphDraftStore.setState({ documents: {} }));
describe("native Graph drafts", () => {
  it("changes condition and binding modes without retaining incompatible fields", () => {
    const s=useGraphDraftStore.getState();s.open("k",{...def,nodes:[{nodeKey:"check",type:"branch",condition:{op:"eq",left:{source:"constant",value:true},right:{source:"constant",value:false}}}]},1);
    s.field("k",["definition","nodes",0,"condition","op"],"all");
    expect(useGraphDraftStore.getState().documents.k.definition.nodes[0].condition).toEqual({op:"all",conditions:[{op:"exists",left:{source:"run_input",path:""}}]});
    s.field("k",["definition","nodes",0,"condition","conditions",0,"left","source"],"constant");
    expect(useGraphDraftStore.getState().documents.k.definition.nodes[0].condition?.conditions?.[0].left).toEqual({source:"constant",value:""});
  });
  it("isolates identity and preserves newer edits after an asynchronous save", () => {
    const key = graphDraftIdentity("ws", "user", "g"); const s=useGraphDraftStore.getState();s.open(key,def,1);s.edit(key,{...def,name:"Submitted"});const submitted=useGraphDraftStore.getState().documents[key].definition;s.edit(key,{...def,name:"New edit"});s.saved(key,submitted,2);
    expect(useGraphDraftStore.getState().documents[key]).toMatchObject({definition:{name:"New edit"},revision:2,dirty:true}); expect(graphDraftIdentity("ws","other","g")).not.toBe(key);
  });
  it("requires proposal application and refuses to overwrite concurrent edits", () => {
    const s=useGraphDraftStore.getState();s.open("k",def,1);s.propose("k",{...def,name:"AI"},def);expect(useGraphDraftStore.getState().documents.k.definition.name).toBe("Research");s.edit("k",{...def,name:"Manual"});expect(()=>s.apply("k")).toThrow();expect(useGraphDraftStore.getState().documents.k.definition.name).toBe("Manual");
  });
  it("undoes a complete edit and retains a conflict when the server revision changes", () => {
    const s=useGraphDraftStore.getState();s.open("k",def,1);s.edit("k",{...def,name:"Local"});s.open("k",{...def,name:"Remote"},2);expect(useGraphDraftStore.getState().documents.k.conflict).toBe(true);s.undo("k");expect(useGraphDraftStore.getState().documents.k.definition.name).toBe("Research");
  });
});
