import React, { useState, useEffect } from 'react';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend } from 'recharts';

const App = () => {
  const [logs, setLogs] = useState([]);
  const [selectedLog, setSelectedLog] = useState(null);

  useEffect(() => {
    fetchLogs();
  }, []);

  const fetchLogs = async () => {
    const response = await fetch('http://localhost:8080/api/logs');
    const data = await response.json();
    setLogs(data);
  };

  const handleRevert = async (id) => {
    await fetch(`/api/revert/${id}`, { method: 'POST' });
    fetchLogs();
  };

  const renderDiff = (oldValue, newValue) => {
    return (
      <div>
        <h4>Old Value:</h4>
        <pre>{JSON.stringify(oldValue, null, 2)}</pre>
        <h4>New Value:</h4>
        <pre>{JSON.stringify(newValue, null, 2)}</pre>
      </div>
    );
  };

  return (
    <div>
      <h1>Audit Log Viewer</h1>
      <div style={{ display: 'flex' }}>
        <div style={{ flex: 1 }}>
          <h2>Logs</h2>
          {logs.map((log) => (
            <div key={log.id} onClick={() => setSelectedLog(log)}>
              {log.operation} on {log.targetTableId} by {log.username}
            </div>
          ))}
        </div>
        <div style={{ flex: 2 }}>
          <h2>Log Details</h2>
          {selectedLog && (
            <div>
              <h3>{selectedLog.operation} on {selectedLog.targetTableId}</h3>
              {renderDiff(selectedLog.oldValue, selectedLog.newValue)}
              <button onClick={() => handleRevert(selectedLog.id)}>Revert</button>
            </div>
          )}
        </div>
      </div>
      <h2>Change History</h2>
      <LineChart width={600} height={300} data={logs}>
        <XAxis dataKey="timestamp" />
        <YAxis />
        <CartesianGrid strokeDasharray="3 3" />
        <Tooltip />
        <Legend />
        <Line type="monotone" dataKey="id" stroke="#8884d8" />
      </LineChart>
    </div>
  );
};

export default App;
