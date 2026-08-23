import React, { useState, useEffect, useCallback } from 'react';
import ReactFlow, { 
  MiniMap, 
  Controls, 
  Background, 
  useNodesState, 
  useEdgesState, 
  addEdge, 
  type Node, 
  type Edge,
  type Connection,
  ReactFlowProvider,
  Handle,
  Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import { Plus, Trash2, Edit2, ArrowLeft, Save, GripVertical, X, AlertTriangle } from 'lucide-react';

/**
 * The agent builder is a design surface only.
 *
 * Saving writes the canvas topology to the `agents` table as JSON and nothing
 * more: there is no Eino dependency in the Go module and no endpoint that
 * executes a saved graph. This banner exists so the feature does not read as
 * finished — every node type from the spec is present and configurable, which
 * makes it look considerably more complete than it is.
 */
const PreviewNotice = () => (
  <div className="flex items-start gap-3 border border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400 px-4 py-3 mb-6">
    <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
    <div className="text-xs leading-relaxed">
      <span className="font-bold uppercase tracking-wider">Design preview</span>
      <span className="mx-2 opacity-40">|</span>
      Agents defined here are saved as graph definitions but{' '}
      <strong className="font-semibold">cannot be executed yet</strong> — the Eino
      runtime is not implemented in the backend. Use this to draft topologies, not
      to run workflows.
    </div>
  </div>
);

const NODE_CATEGORIES = [
  {
    name: 'Core Operational',
    nodes: [
      { type: 'ChatModel', label: 'ChatModel', color: 'bg-blue-500/20 border-blue-500/50 text-blue-500' },
      { type: 'ToolsNode', label: 'ToolsNode', color: 'bg-blue-500/20 border-blue-500/50 text-blue-500' },
      { type: 'ChatTemplate', label: 'ChatTemplate', color: 'bg-blue-500/20 border-blue-500/50 text-blue-500' },
    ]
  },
  {
    name: 'Data & Context',
    nodes: [
      { type: 'Retriever', label: 'Retriever', color: 'bg-emerald-500/20 border-emerald-500/50 text-emerald-500' },
      { type: 'Embedding', label: 'Embedding', color: 'bg-emerald-500/20 border-emerald-500/50 text-emerald-500' },
      { type: 'Lambda', label: 'Lambda', color: 'bg-emerald-500/20 border-emerald-500/50 text-emerald-500' },
    ]
  },
  {
    name: 'Specialized Patterns',
    nodes: [
      { type: 'ReActAgent', label: 'ReAct Agent', color: 'bg-purple-500/20 border-purple-500/50 text-purple-500' },
      { type: 'SequentialAgent', label: 'Sequential Agent', color: 'bg-purple-500/20 border-purple-500/50 text-purple-500' },
      { type: 'ParallelAgent', label: 'Parallel Agent', color: 'bg-purple-500/20 border-purple-500/50 text-purple-500' },
      { type: 'LoopAgent', label: 'Loop Agent', color: 'bg-purple-500/20 border-purple-500/50 text-purple-500' },
    ]
  },
  {
    name: 'Graph Plumbing',
    nodes: [
      { type: 'START', label: 'START', color: 'bg-amber-500/20 border-amber-500/50 text-amber-500' },
      { type: 'END', label: 'END', color: 'bg-amber-500/20 border-amber-500/50 text-amber-500' },
      { type: 'Branch', label: 'Branch', color: 'bg-amber-500/20 border-amber-500/50 text-amber-500' },
    ]
  }
];

const Sidebar = () => {
  const onDragStart = (event: React.DragEvent, nodeType: string, label: string) => {
    event.dataTransfer.setData('application/reactflow', nodeType);
    event.dataTransfer.setData('application/reactflow-label', label);
    event.dataTransfer.effectAllowed = 'move';
  };

  return (
    <div className="w-64 border-r border-border bg-card flex flex-col h-full overflow-y-auto">
      <div className="p-4 border-b border-border bg-muted/30">
        <h3 className="font-bold text-sm uppercase tracking-wider text-muted-foreground">Node Palette</h3>
        <p className="text-xs text-muted-foreground mt-1">Drag nodes to the canvas</p>
      </div>
      <div className="p-4 space-y-6">
        {NODE_CATEGORIES.map((cat, idx) => (
          <div key={idx} className="space-y-3">
            <h4 className="text-xs font-bold text-foreground/80 uppercase tracking-wider">{cat.name}</h4>
            <div className="space-y-2">
              {cat.nodes.map((node) => (
                <div 
                  key={node.type}
                  onDragStart={(e) => onDragStart(e, node.type, node.label)}
                  draggable
                  className={`flex items-center gap-2 p-2 border cursor-grab hover:brightness-110 transition-all ${node.color}`}
                >
                  <GripVertical className="w-4 h-4 opacity-50" />
                  <span className="text-sm font-semibold">{node.label}</span>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

const SettingsPanel = ({ selectedNode, updateNodeData, closePanel }: { selectedNode: Node, updateNodeData: (id: string, data: any) => void, closePanel: () => void }) => {
  if (!selectedNode) return null;

  const handleChange = (field: string, value: any) => {
    updateNodeData(selectedNode.id, {
      ...selectedNode.data,
      settings: {
        ...(selectedNode.data.settings || {}),
        [field]: value
      }
    });
  };

  const settings = selectedNode.data.settings || {};

  const renderFields = () => {
    switch (selectedNode.data.type) {
      case 'ChatModel':
        return (
          <>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Model Provider</label>
              <select className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.provider || 'Local'} onChange={e => handleChange('provider', e.target.value)}>
                <option value="Local">Local (Ollama)</option>
                <option value="OpenAI">OpenAI</option>
                <option value="Anthropic">Anthropic</option>
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Model Name</label>
              <input type="text" className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.modelName || ''} onChange={e => handleChange('modelName', e.target.value)} placeholder="e.g. llama3" />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Temperature ({settings.temperature || 0.7})</label>
              <input type="range" min="0" max="1" step="0.1" className="w-full" value={settings.temperature || 0.7} onChange={e => handleChange('temperature', parseFloat(e.target.value))} />
            </div>
          </>
        );
      case 'ToolsNode':
        return (
          <>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Available Tools</label>
              <input type="text" className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.tools || ''} onChange={e => handleChange('tools', e.target.value)} placeholder="Comma separated: web_search, send_email" />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Timeout (ms)</label>
              <input type="number" className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.timeout || 5000} onChange={e => handleChange('timeout', parseInt(e.target.value))} />
            </div>
          </>
        );
      case 'Retriever':
        return (
          <>
             <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Data Source</label>
              <select className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.source || 'Email DB'} onChange={e => handleChange('source', e.target.value)}>
                <option value="Email DB">Email DB</option>
                <option value="Local Docs">Local Docs</option>
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Top K Results</label>
              <input type="number" className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={settings.topK || 5} onChange={e => handleChange('topK', parseInt(e.target.value))} />
            </div>
          </>
        );
      case 'ChatTemplate':
      case 'Lambda':
      case 'Branch':
        return (
          <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Code / Template</label>
              <textarea className="w-full h-40 bg-background border border-border p-2 text-sm outline-none focus:border-primary font-mono" value={settings.code || ''} onChange={e => handleChange('code', e.target.value)} placeholder="Enter configuration or logic..." />
          </div>
        );
      case 'START':
      case 'END':
         return (
          <div className="space-y-1">
              <label className="text-xs font-bold text-muted-foreground uppercase">Schema Definition</label>
              <textarea className="w-full h-40 bg-background border border-border p-2 text-sm outline-none focus:border-primary font-mono" value={settings.schema || ''} onChange={e => handleChange('schema', e.target.value)} placeholder='e.g., {"type": "object", "properties": {}}' />
          </div>
        );
      default:
        return (
          <div className="text-sm text-muted-foreground italic">
            General settings for {selectedNode.data.label}
          </div>
        );
    }
  };

  return (
    <div className="w-80 border-l border-border bg-card flex flex-col h-full">
      <div className="p-4 border-b border-border bg-muted/30 flex justify-between items-center">
        <div>
          <h3 className="font-bold text-sm uppercase tracking-wider text-muted-foreground">Node Settings</h3>
          <p className="text-xs text-primary font-bold mt-1">{selectedNode.data.label}</p>
        </div>
        <button onClick={closePanel} className="p-1 hover:bg-accent text-muted-foreground transition-colors">
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="p-4 space-y-4 overflow-y-auto flex-1">
        <div className="space-y-1">
          <label className="text-xs font-bold text-muted-foreground uppercase">Node Label</label>
          <input type="text" className="w-full bg-background border border-border p-2 text-sm outline-none focus:border-primary" value={selectedNode.data.label || ''} onChange={e => updateNodeData(selectedNode.id, { ...selectedNode.data, label: e.target.value })} />
        </div>
        <hr className="border-border" />
        {renderFields()}
      </div>
    </div>
  );
};

const CustomNode = ({ data, selected }: any) => {
  let bgClass = "bg-card";
  let borderClass = selected ? "border-primary shadow-[0_0_0_1px_rgba(var(--primary),0.5)]" : "border-border";
  
  if (data.type === 'START' || data.type === 'END' || data.type === 'Branch') {
    bgClass = "bg-amber-500/10";
    borderClass = selected ? "border-amber-500" : "border-amber-500/30";
  } else if (['ChatModel', 'ToolsNode', 'ChatTemplate'].includes(data.type)) {
    bgClass = "bg-blue-500/10";
    borderClass = selected ? "border-blue-500" : "border-blue-500/30";
  } else if (['Retriever', 'Embedding', 'Lambda'].includes(data.type)) {
    bgClass = "bg-emerald-500/10";
    borderClass = selected ? "border-emerald-500" : "border-emerald-500/30";
  } else if (['ReActAgent', 'SequentialAgent', 'ParallelAgent', 'LoopAgent'].includes(data.type)) {
    bgClass = "bg-purple-500/10";
    borderClass = selected ? "border-purple-500" : "border-purple-500/30";
  }

  return (
    <div className={`px-4 py-2 border-2 ${bgClass} ${borderClass} min-w-[150px]`}>
      {data.type !== 'START' && <Handle type="target" position={Position.Left} className="w-2 h-2 bg-muted-foreground border-background" />}
      <div className="font-bold text-sm text-foreground">{data.label}</div>
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground mt-1">{data.type}</div>
      {data.type !== 'END' && <Handle type="source" position={Position.Right} className="w-2 h-2 bg-muted-foreground border-background" />}
    </div>
  );
};

const nodeTypes = {
  custom: CustomNode,
};

const FlowEditor = ({ initialNodes, initialEdges, onSave, onBack, currentAgent, setCurrentAgent }: any) => {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes || []);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges || []);
  const [reactFlowInstance, setReactFlowInstance] = useState<any>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  const onConnect = useCallback((params: Edge | Connection) => setEdges((eds) => addEdge(params, eds)), [setEdges]);

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();

      const type = event.dataTransfer.getData('application/reactflow');
      const label = event.dataTransfer.getData('application/reactflow-label');

      if (typeof type === 'undefined' || !type) {
        return;
      }

      const position = reactFlowInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });
      const newNode: Node = {
        id: `node_${new Date().getTime()}`,
        type: 'custom',
        position,
        data: { label: label, type: type, settings: {} },
      };

      setNodes((nds) => nds.concat(newNode));
      setSelectedNodeId(newNode.id);
    },
    [reactFlowInstance, setNodes]
  );

  const updateNodeData = (id: string, data: any) => {
    setNodes((nds) =>
      nds.map((node) => {
        if (node.id === id) {
          node.data = data;
        }
        return node;
      })
    );
  };

  const selectedNode = nodes.find(n => n.id === selectedNodeId) || null;

  return (
    <div className="flex flex-col h-full bg-background text-foreground animate-in fade-in duration-200">
      <div className="h-14 border-b border-border flex items-center px-4 justify-between bg-card/50 shrink-0">
        <div className="flex items-center gap-4">
          <button onClick={onBack} className="p-2 hover:bg-accent transition-colors">
            <ArrowLeft className="w-5 h-5 text-muted-foreground" />
          </button>
          <div className="flex flex-col">
            <input 
              type="text" 
              value={currentAgent?.name || ''} 
              onChange={(e) => setCurrentAgent({ ...currentAgent, name: e.target.value })}
              className="bg-transparent font-bold outline-none text-sm" 
            />
            <input 
              type="text" 
              value={currentAgent?.description || ''} 
              onChange={(e) => setCurrentAgent({ ...currentAgent, description: e.target.value })}
              className="bg-transparent text-xs text-muted-foreground outline-none w-64" 
            />
          </div>
        </div>
        <div className="flex items-center gap-4">
          <span
            title="Saving stores the graph definition only; there is no runtime to execute it yet"
            className="hidden md:flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider text-amber-600 dark:text-amber-400 border border-amber-500/40 bg-amber-500/10 px-2 py-1"
          >
            <AlertTriangle className="w-3 h-3" /> Design preview — not executable
          </span>
          <button onClick={() => onSave(nodes, edges)} className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 text-xs font-semibold shadow hover:opacity-90 transition-opacity">
            <Save className="w-4 h-4" /> Save Agent
          </button>
        </div>
      </div>
      <div className="flex-1 w-full flex overflow-hidden">
        <Sidebar />
        <div className="flex-1 relative">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onInit={setReactFlowInstance}
            onDrop={onDrop}
            onDragOver={onDragOver}
            onNodeClick={(_, node) => setSelectedNodeId(node.id)}
            onPaneClick={() => setSelectedNodeId(null)}
            nodeTypes={nodeTypes}
            fitView
          >
            <Controls />
            <MiniMap />
            <Background gap={12} size={1} />
          </ReactFlow>
        </div>
        {selectedNode && (
          <SettingsPanel selectedNode={selectedNode} updateNodeData={updateNodeData} closePanel={() => setSelectedNodeId(null)} />
        )}
      </div>
    </div>
  );
};


export const AgentManager = () => {
  const [agents, setAgents] = useState<any[]>([]);
  const [view, setView] = useState<'list' | 'edit'>('list');
  const [currentAgent, setCurrentAgent] = useState<any>(null);
  const [initialNodes, setInitialNodes] = useState<Node[]>([]);
  const [initialEdges, setInitialEdges] = useState<Edge[]>([]);

  const fetchAgents = async () => {
    try {
      const res = await fetch('/api/agents');
      if (res.ok) {
        const data = await res.json();
        setAgents(data || []);
      }
    } catch (e) {
      console.error('Failed to fetch agents', e);
    }
  };

  useEffect(() => {
    if (view === 'list') {
      fetchAgents();
    }
  }, [view]);

  const handleCreateNew = () => {
    setCurrentAgent({ name: 'New Agent', description: 'A new AI agent' });
    setInitialNodes([{ id: 'start', position: { x: 100, y: 200 }, data: { label: 'START', type: 'START', settings: {} }, type: 'custom' }]);
    setInitialEdges([]);
    setView('edit');
  };

  const handleEdit = (agent: any) => {
    setCurrentAgent(agent);
    try {
      const schema = JSON.parse(agent.schemaJson);
      setInitialNodes(schema.nodes || []);
      setInitialEdges(schema.edges || []);
    } catch (e) {
      setInitialNodes([]);
      setInitialEdges([]);
    }
    setView('edit');
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this agent?')) return;
    try {
      await fetch(`/api/agents?id=${id}`, { method: 'DELETE' });
      fetchAgents();
    } catch (e) {
      console.error('Delete failed', e);
    }
  };

  const handleSave = async (nodes: Node[], edges: Edge[]) => {
    const schemaJson = JSON.stringify({ nodes, edges });
    const payload = { ...currentAgent, schemaJson };
    try {
      const res = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        const saved = await res.json();
        setCurrentAgent(saved);
        alert('Agent saved successfully!');
      }
    } catch (e) {
      console.error('Save failed', e);
    }
  };

  if (view === 'edit') {
    return (
      <ReactFlowProvider>
        <FlowEditor 
          initialNodes={initialNodes}
          initialEdges={initialEdges}
          onSave={handleSave}
          onBack={() => setView('list')}
          currentAgent={currentAgent}
          setCurrentAgent={setCurrentAgent}
        />
      </ReactFlowProvider>
    );
  }

  return (
    <div className="p-6 overflow-auto h-full bg-background text-foreground">
      <div className="flex justify-between items-center mb-6">
        <div>
          <h2 className="text-2xl font-bold">AI Agents</h2>
          <p className="text-sm text-muted-foreground mt-1">Draft agent topologies for the planned Eino runtime.</p>
        </div>
        <button onClick={handleCreateNew} className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 text-sm font-semibold shadow hover:opacity-90 transition-opacity">
          <Plus className="w-4 h-4" /> Create New Agent
        </button>
      </div>

      <PreviewNotice />

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {agents.map((agent) => (
          <div key={agent.id} className="bg-card border border-border p-5 shadow-sm group hover:border-primary/50 transition-colors flex flex-col h-40">
            <div className="flex justify-between items-start mb-2">
              <h3 className="font-bold text-lg truncate">{agent.name}</h3>
              <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button onClick={() => handleEdit(agent)} className="p-1.5 hover:bg-accent text-muted-foreground hover:text-primary transition-colors">
                  <Edit2 className="w-4 h-4" />
                </button>
                <button onClick={() => handleDelete(agent.id)} className="p-1.5 hover:bg-destructive/10 text-muted-foreground hover:text-destructive transition-colors">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
            <p className="text-sm text-muted-foreground line-clamp-2 flex-1">{agent.description}</p>
            <div className="mt-4 pt-4 border-t border-border flex justify-between items-center text-xs font-mono text-muted-foreground">
              <span>ID: {agent.id.substring(0, 8)}...</span>
              <span
                title="Saved as a graph definition; not executable yet"
                className="bg-amber-500/10 text-amber-600 dark:text-amber-400 px-2 py-0.5 font-bold"
              >
                DRAFT
              </span>
            </div>
          </div>
        ))}
        {agents.length === 0 && (
          <div className="col-span-full py-20 flex flex-col items-center justify-center text-muted-foreground italic border-2 border-dashed border-border">
            <p>No agents defined yet. Create one to get started.</p>
          </div>
        )}
      </div>
    </div>
  );
};
